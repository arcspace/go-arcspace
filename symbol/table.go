package symbol

import (
	"bytes"
	"sync"

	"github.com/arcspace/go-cedar/bufs"
	"github.com/dgraph-io/badger/v3"
)

func (id ID) WriteTo(io []byte) []byte {
	return append(io, // big endian marshal
		byte(uint64(id)>>32),
		byte(uint64(id)>>24),
		byte(uint64(id)>>16),
		byte(uint64(id)>>8),
		byte(id))
}

func (id *ID) ReadFrom(in []byte) int {
	*id = ID(
		(uint64(in[0]) << 32) |
			(uint64(in[1]) << 24) |
			(uint64(in[2]) << 16) |
			(uint64(in[3]) << 8) |
			(uint64(in[4])))

	return IDSz
}

const (
	xValueIndex = byte(0xFE)
	xNextID     = byte(0xFF)
)

func openTable(db *badger.DB, opts TableOpts) (Table, error) {
	var err error

	if opts.Issuer == nil {
		opts.Issuer, err = openIssuer(db, opts)
		opts.IssuerOwned = true
	}
	if err != nil {
		return nil, err
	}

	st := &symbolTable{
		opts:          opts,
		db:            db,
		curBufPoolIdx: -1,
		valueCache:    make(map[uint64]kvEntry, opts.WorkingSizeHint),
		tokenCache:    make(map[ID]kvEntry, opts.WorkingSizeHint),
	}

	return st, nil
}

func (st *symbolTable) Issuer() Issuer {
	return st.opts.Issuer
}

func (st *symbolTable) Close() {
	if st.opts.IssuerOwned {
		st.opts.Issuer.Close()
	}
	st.opts.Issuer = nil
	st.valueCache = nil
	st.tokenCache = nil
	st.bufPools = nil
	st.db = nil
}

type kvEntry struct {
	symID   ID
	poolIdx int32
	poolOfs int32
	len     int32
}

func (st *symbolTable) equals(kv *kvEntry, buf []byte) bool {
	sz := int32(len(buf))
	if sz != kv.len {
		return false
	}
	return bytes.Equal(st.bufPools[kv.poolIdx][kv.poolOfs:kv.poolOfs+sz], buf)
}

func (st *symbolTable) bufForEntry(kv *kvEntry) []byte {
	if kv.symID == 0 {
		return nil
	}
	return st.bufPools[kv.poolIdx][kv.poolOfs : kv.poolOfs+kv.len]
}

// symbolTable implements symbol.Table
type symbolTable struct {
	opts          TableOpts
	db            *badger.DB
	valueCacheMu  sync.RWMutex       // Protects valueCache
	valueCache    map[uint64]kvEntry // Maps a entry value hash to a kvEntry
	tokenCacheMu  sync.RWMutex       // Protects tokenCache
	tokenCache    map[ID]kvEntry     // Maps an ID ("token") to an entry
	curBufPool    []byte
	curBufPoolSz  int32
	curBufPoolIdx int32
	bufPools      [][]byte
}

func (st *symbolTable) getIDFromCache(buf []byte) ID {
	hash := bufs.HashBuf(buf)

	st.valueCacheMu.RLock()
	defer st.valueCacheMu.RUnlock()

	kv, found := st.valueCache[hash]
	for found {
		if st.equals(&kv, buf) {
			return kv.symID
		}
		hash++
		kv, found = st.valueCache[hash]
	}

	return 0
}

func (st *symbolTable) allocAndBindToID(buf []byte, bindID ID) kvEntry {
	hash := bufs.HashBuf(buf)

	st.valueCacheMu.Lock()
	defer st.valueCacheMu.Unlock()

	kv, found := st.valueCache[hash]
	for found {
		if st.equals(&kv, buf) {
			break
		}
		hash++
		kv, found = st.valueCache[hash]
	}

	// No-op if already present
	if found && kv.symID == bindID {
		return kv
	}

	// At this point we know [hash] will be the destination element
	// Add a copy of the buf in our backing buf (in the heap).
	// If we run out of space in our pool, we start a new pool
	kv.symID = bindID
	{
		kv.len = int32(len(buf))
		if int(st.curBufPoolSz+kv.len) > cap(st.curBufPool) {
			allocSz := max(st.opts.PoolSz, kv.len)
			st.curBufPool = make([]byte, allocSz)
			st.curBufPoolSz = 0
			st.curBufPoolIdx++
			st.bufPools = append(st.bufPools, st.curBufPool)
		}
		kv.poolIdx = st.curBufPoolIdx
		kv.poolOfs = st.curBufPoolSz
		copy(st.curBufPool[kv.poolOfs:kv.poolOfs+kv.len], buf)
		st.curBufPoolSz += kv.len
	}

	// Place the now-backed copy at the open hash spot and return the alloced value
	st.valueCache[hash] = kv

	st.tokenCacheMu.Lock()
	st.tokenCache[kv.symID] = kv
	st.tokenCacheMu.Unlock()

	return kv
}

func (st *symbolTable) GetSymbolID(val []byte, autoIssue bool) ID {
	symID := st.getIDFromCache(val)
	if symID != 0 {
		return symID
	}

	symID = st.getsetValueIDPair(val, 0, autoIssue)
	return symID
}

func (st *symbolTable) SetSymbolID(val []byte, symID ID) ID {
	// If symID == 0, then behave like GetSymbolID(val, true)
	return st.getsetValueIDPair(val, symID, symID == 0)
}

// getsetValueIDPair loads and returns the ID for the given value, and/or writes the ID and value assignment to the db,
// also updating the cache in the process.
//
//  if symID == 0:
//    if the given value has an existing value-ID association:
//        the existing ID is cached and returned (mapID is ignored).
//    if the given value does NOT have an existing value-ID association:
//        if mapID == false, the call has no effect and 0 is returned.
//        if mapID == true, a new ID is issued and new value-to-ID and ID-to-value assignments are written,
//
//  if symID != 0:
//      if mapID == false, a new value-to-ID assignment is (over)written and any existing ID-to-value assignment remains.
//      if mapID == true, both value-to-ID and ID-to-value assignments are (over)written.
//
func (st *symbolTable) getsetValueIDPair(val []byte, symID ID, mapID bool) ID {

	if st.db == nil {
		if symID == 0 && mapID {
			symID, _ = st.opts.Issuer.IssueNextID()
		}
	} else {
		txn := st.db.NewTransaction(true)
		defer txn.Discard()

		// The value index is placed after the ID index
		var (
			keyBuf [128]byte
			idBuf  [8]byte
			err    error
		)

		keyBuf[0] = st.opts.DbKeyPrefix
		keyBuf[1] = 0xFF
		keyBuf[2] = xValueIndex
		valKey := append(keyBuf[:3], val...)

		var existingID ID
		if symID == 0 || !mapID {

			// Lookup the given value and get its existing ID
			item, err := txn.Get(valKey)
			if err == nil {
				item.Value(func(buf []byte) error {
					if len(buf) == IDSz {
						existingID.ReadFrom(buf)
					}
					return nil
				})
			}
		}

		reassignID := false
		reassignVal := false

		if symID == 0 {
			if existingID != 0 {
				symID = existingID
			} else if mapID {
				symID, err = st.opts.Issuer.IssueNextID()
				if err == nil {
					reassignID = true
					reassignVal = true
				}
			}
		} else {
			if existingID == 0 {
				reassignVal = true
				reassignID = true
			} else if symID != existingID {
				reassignVal = true
				if mapID {
					symID = existingID
					reassignID = true
				}
			}
		}

		// If applicable, flush the new kv assignment change to the db
		for err == nil && (reassignID || reassignVal) {

			// set (value => ID) entry
			idBuf[0] = st.opts.DbKeyPrefix
			idKey := symID.WriteTo(idBuf[:1])
			err := txn.Set(valKey, idKey[1:])

			if err == nil {
				if reassignID {
					err = txn.Set(idKey, val)
				}
				if err == nil {
					err = txn.Commit()
				}
			}

			if err != badger.ErrConflict {
				break
			}

			err = nil
			txn.Discard()
			txn = st.db.NewTransaction(true)
		}

		if err != nil {
			panic(err)
		}
	}

	// Update the cache
	if symID != 0 {
		st.allocAndBindToID(val, symID)
	}
	return symID
}

func (st *symbolTable) LookupID(symID ID) []byte {
	if symID == 0 {
		return nil
	}

	st.tokenCacheMu.RLock()
	kv, found := st.tokenCache[symID]
	st.tokenCacheMu.RUnlock()

	// If we have the ID in the cache, then use that (hopefully most of the time).
	// Otherwise, look up symID in the db and add it to the cache pool.
	if !found && st.db != nil {
		txn := st.db.NewTransaction(false)
		defer txn.Discard()

		var idBuf [8]byte
		idBuf[0] = st.opts.DbKeyPrefix
		tokenKey := symID.WriteTo(idBuf[:1])
		item, err := txn.Get(tokenKey)
		if err == nil {
			item.Value(func(val []byte) error {
				kv = st.allocAndBindToID(val, symID)
				return nil
			})
		}
	}

	// At this point, if symID wasn't found, kv will be zero and causing nil to be returned
	return st.bufForEntry(&kv)
}

func max(a, b int32) int32 {
	if a > b {
		return a
	} else {
		return b
	}
}
