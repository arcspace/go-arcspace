package planet

import "github.com/genesis3systems/go-cedar/process"

/*
packages

	planet
	    planet interfaces and support utils
	planet/host
	    an implementation of planet.Host & planet.HostSession
	planet/grpc_server
		implements a grpc server that consumes a planet.Host instance
	planet/apps
		implementations of planet.App


	phost process.Context model:
		* Host
		    * HostHomePlanet
		        * hostSess
		        * cell_101
		        * cell_104
		    * GrpcServer
		        * grpcSess
		            * grpc <- hostSess.Outbox
		            * grpc -> hostSess.Inbox
		        * grpcSess
		            * grpc <- hostSess.Outbox
		            * grpc -> hostSess.Inbox


	May this project be dedicated to God, for all other things are darkness or imperfection.
	May these hands and this mind be blessed with Holy Spirit and Holy Purpose.
	May I be an instrument for manifesting software that serves the light and used to manifest joy at the largest scale possible.
	May the blocks to this mission dissolve into light amidst God's will.

	~ Dec 25th, 2021

*/

// TID identifies a specific planet, node, or transaction.
//
// Unless otherwise specified a TID in the wild should always be considered read-only.
type TID []byte

// TIDBuf is the blob version of a TID
type TIDBuf [TIDBinaryLen]byte



type Context interface {
	process.Context
}



type TypeRegistry interface {

    // Resolves and then registers each given def, returning the resolved defs in-place if successful.
    //
    // Resolving a AttrSchema means:
    //    1) all name identifiers have been resolved to their corresponding host-dependent symbol IDs.
    //    2) all "InheritsFrom" types and fields have been "flattened" into the form 
    //
    // See MsgOp_ResolveAndRegister
    ResolveAndRegister(defs *Defs) error
    
    // Returns the resolved AttrSchema for the given cell type ID.
	GetSchemaByID(schemaID uint32) (*AttrSchema, error)
}





// Host is the highest level controller. 
// Child processes attach to it and start new host sessions as needed.
type Host interface {
	Context
		
	HostPlanet() Planet
	
	// Registers an App for invocation by its AppURI and DataModelURIs.
	RegisterApp(app App) error
		
	// Selects an App, typically based on schema.DataModelURI (or schema.AppURI if given).
	// The given schema is READ ONLY.
	SelectAppForSchema(schema *AttrSchema) (App, error)
		
    StartNewSession() (HostSession, error)
    
}


// HostEndpoint offers Msg pipe endpoint access, allowing it to be lifted over any Msg transport layer.
type HostEndpoint interface {
	Context
	
	// This provides Msg pipe endpoint access for lifting over a Msg transport layer.
	// This is intended to be consumed by a grpc (or other io layer).
	Inbox() chan *Msg
	Outbox() chan *Msg
	
}

// HostSession in an open session instance with a Host.
// HostSession is intended to be consumed by a Msg transport layer that in turn is intended
// to be consumed by an implementation of a client.HostSession.
type HostSession interface {
	HostEndpoint

	// Threadsafe
	TypeRegistry
	
	LoggedIn() User
	
	//UserPlanet() Planet
	
    
}



// Planet is content and governance enclosure.
// A Planet is 1:1 with a KV database model, which works out well for efficiency and performance.
type Planet interface {
	
	// A Planet instance is a child process of a host
	Context
	
	PlanetID() uint64
	
	// A planet offers a persistent symbol table, allowing efficient compression of byte symbols into uint64s
	GetSymbolID(value []byte, autoIssue bool) (ID uint64)
	LookupID(ID uint64) []byte

	//GetCell(ID CellID) (CellInstance, error)

	// BlobStore offers access to this planet's blob store (referenced via ValueType_BlobID).
	//blob.Store
	
}



type CellID uint64
func (ID CellID) U64() uint64 { return uint64(ID) }

type CellReq struct {
	ReqID        uint64
	Parent       *CellReq
	PlanetID     uint64
	Target       CellID
	CellURI      string
	PinSchema    *AttrSchema
	PinChildren  []*AttrSchema
	App          App         // App responding to this request
	AppItem      interface{} // Optionally set by App.ResolveRequest()
}

// Signals to use the default App for a given AttrSchema DataModelURI.
// See AttrSchema.AppURI in planet.proto 
const DefaultAppForDataModel = "."

// App creates a new Channel instance on demand when Planet.GetChannel() is called.
// App and AppChannel consume the Planet and Channel interfaces to perform specialized functionality.
// In general, a channel app should be specialized for a specific, taking inspiration from the legacy of unix util way-of-thinking.
type App interface {

	// Identifies this App and usually has the form: "{domain_name}/{app_identifier}/v{MAJOR}.{MINOR}.{REV}"
	AppURI() string
	
	// Lists data model URIs (i.e. data protocols) that this app handles.
	// When the host session receives a client request with a specific data model URI, it will route it to the app that handles it.
	DataModelURIs() []string
		
	// Attaches the given request to the requested cell until it is canceled or completed.
	//PinCell(req *CellReq) (CellSub, error)
	
	// Resolves the given request to final target Planet and CellID that used to   
	// An App typically sets an AppItem value so that PushCellState() are ready to work.
	ResolveRequest(req *CellReq) error
	
	// Called when the sub is new and needs a state refresh.  
	// Makes calls to sub.PushUpdate() to dispatch state.
	// Called on the goroutine owned by the the target cell. 
	PushCellState(sub CellSub) error
	
	//GetLabel(sub CellSub) string

	// Creates a new App instance that is bound to the given channel and starts it as a "child process" of the host / bound channel
	// Blocks until the new AppChannel is in a valid and ready state.
	// Typically, the returned AppChannel is upcast to the desired/presumed Channel interface.
	//StartAppInstance(sess CellSession) (AppCell, error)
}





type CellSub interface {

	Req() *CellReq
			
	// Pushes copies of the msgs in the given batch to this sub, blocking until complete or canceled.
	PushUpdate(batch MsgBatch) error
	
	//PushError(err error)
	
}


type User interface {
	HomePlanet() Planet
}

// MsgBatch is an ordered list os Msgs
// See NewMsgBatch()
type MsgBatch struct {
	Msgs []*Msg
}



// // PinnedCell?
// // CellAccess?
// // CellReq?
// // CellPin?
// // CellSession?
// // CellSub?
// //
// // OpenCell reflects an open cell bound to the governance of its "home" planet.
// type OpenCell interface {

// 	// A channel instance is a child process of its home Planet
// 	Context
	
// 	// Identifies this App's functionality and author.
// 	ParentApp() App

	// // Returns the Planet hosting this Channel
	// HomePlanet() Planet
	
	// // GenesisTID returns the genesis TID of this Channel, in effect identifying this channel.
	// GenesisTID() TID
	
	// // NodeID returns the symbolic representation of this Channel aka "root" NodeID of a channel.
	// RootNode() NodeID
	
	//SnapshotChNode(nodeID symbol.ID, ChNode) ChNodeSnapshot
	// ChNode(nodeID symbol.ID) ChNode
	
	// LoadAttrs gets the most up to date values of the requested attr IDs.
	// Returns the number of attrs that were not found or could be exported to the target dst value type.
	// If fromID == 0, then all participant members are selected, otherwise only attrs set from the specified participant are selected.
	// If nodeID == 0, then all nodes in this channel are selected, otherwise only attrs set for the specified nodeID are selected.
	//LoadAttrs(fromID, nodeID symbol.ID, srcAttrs []symbol.ID, dstVals []interface{}) (int, error)
	
	// Called when the Channel is starting up.
	// The channel is presumed to read from the state and serve channel app clients specialized for that app type.
	//OnStart(state ChState)

	// NewTransaction() (TxWriter, error)
	
// }

