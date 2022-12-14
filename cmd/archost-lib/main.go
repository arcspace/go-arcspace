// go/cmd/archost-lib/main.go
package main

import "C"

import (
	"fmt"
	"sync"

	"github.com/arcspace/go-arcspace/arc"
	"github.com/arcspace/go-arcspace/arc/archost"
	"github.com/arcspace/go-arcspace/arc/host"
	"github.com/arcspace/go-arcspace/arc/lib_service"
)

var (
	gLibSession lib_service.LibSession
	gLibService lib_service.LibService
)

//export Call_SessionBegin
func Call_SessionBegin(userID, userDataPath, sharedCachePath string) int64 {
	if gLibSession != nil {
		return 0
	}

	hostOpts := host.DefaultHostOpts()
	hostOpts.CachePath = sharedCachePath
	hostOpts.StatePath = userDataPath
	h := archost.StartNewHost(hostOpts)

	opts := lib_service.DefaultLibServiceOpts()
	gLibService = opts.NewLibService()
	err := gLibService.StartService(h)
	if err == nil {
		gLibSession, err = gLibService.NewLibSession()
	}

	if err != nil {
		errMsg := fmt.Sprintf("failed to start LibService: %v", err)
		h.Error(errMsg)
		h.Close()
		return 0
	}

	return int64(12345)
}

//export Call_SessionEnd
func Call_SessionEnd(sessID int64) {
	sess := gLibSession
	if sess == nil {
		return
	}

	sess.Close()
	gLibSession = nil
}

//export Call_Shutdown
func Call_Shutdown() {
	srv := gLibService
	if srv == nil {
		return
	}

	gLibService = nil
	gLibSession = nil

	// Closing the host will cause the lib server to detach
	srv.Host().Close()
	<-srv.Done()
}

//export Call_PushMsg
func Call_PushMsg(msg_pb []byte) int64 {
	sess := gLibSession
	if sess == nil {
		return -1
	}

	msg := arc.NewMsg()
	if err := msg.Unmarshal(msg_pb); err != nil {
		panic(err)
	}
	sess.EnqueueIncoming(msg)
	return 0
}

//export Call_WaitOnMsg
func Call_WaitOnMsg(msg_pb *[]byte) int64 {
	sess := gLibSession
	if sess == nil {
		return -1
	}

	sess.DequeueOutgoing(msg_pb)
	return 0
}

//export Call_Realloc
func Call_Realloc(buf *[]byte, newLen int64) int64 {
	sess := gLibSession
	if sess == nil {
		return -1
	}

	sess.Realloc(buf, newLen)
	return 0
}

type UnityRenderingExtEventType int32

const (
	kUnityRenderingExtEventSetStereoTarget     UnityRenderingExtEventType = iota // issued during SetStereoTarget and carrying the current 'eye' index as parameter
	kUnityRenderingExtEventSetStereoEye                                          // issued during stereo rendering at the beginning of each eye's rendering loop. It carries the current 'eye' index as parameter
	kUnityRenderingExtEventStereoRenderingDone                                   // issued after the rendering has finished
	kUnityRenderingExtEventBeforeDrawCall                                        // issued during BeforeDrawCall and carrying UnityRenderingExtBeforeDrawCallParams as parameter
	kUnityRenderingExtEventAfterDrawCall                                         // issued during AfterDrawCall. This event doesn't carry any parameters
	kUnityRenderingExtEventCustomGrab                                            // issued during GrabIntoRenderTexture since we can't simply copy the resources
	//      when custom rendering is used - we need to let plugin handle this. It carries over
	//      a UnityRenderingExtCustomBlitParams params = { X, source, dest, 0, 0 } ( X means it's irrelevant )
	kUnityRenderingExtEventCustomBlit // issued by plugin to insert custom blits. It carries over UnityRenderingExtCustomBlitParams as param.
	kUnityRenderingExtEventUpdateTextureBegin
	kUnityRenderingExtEventUpdateTextureEnd

	// keep this last
	kUnityRenderingExtEventCount
	kUnityRenderingExtUserEventsStart = kUnityRenderingExtEventCount
)

type FrameBuf struct {
	width       int32
	height      int32
	bytesPerRow int32
	pixels      []byte
}

var curX int32
var curY int32

var gCtx []FrameBuf

//export CreateCtx
func CreateCtx() int32 {
	idx := len(gCtx)
	gCtx = append(gCtx, FrameBuf{})

	curX = 0
	curY = 0

	return int32(idx)
}

//export ResizeCtx
func ResizeCtx(ctxID int32, width int32, height int32, bytesPerRow int32, buf []byte) {
	gCtx[ctxID] = FrameBuf{
		width:       width,
		height:      height,
		bytesPerRow: bytesPerRow,
		pixels:      buf,
	}
}

//export TextureUpdateCallback
func TextureUpdateCallback(eventID UnityRenderingExtEventType, data int64) {

	switch eventID {
	case kUnityRenderingExtEventUpdateTextureBegin:
	case kUnityRenderingExtEventUpdateTextureEnd:
		break
	}
}

//export GetTextureUpdateCallback
func GetTextureUpdateCallback() func(eventID UnityRenderingExtEventType, data int64) {
	return TextureUpdateCallback
}

//export RenderFrame
func RenderFrame(fbID int32) {
	frame := gCtx[fbID]
	if curX > frame.width {
		curX = 0
		curY = (curY + 1) % frame.height
	} else {
		curX++
	}

	offset := (int32)(curX*4 + curY*frame.bytesPerRow)
	// b32 := &(frame.pixels[offset])
	// *b32 = 0x7F3F8F7F;
	frame.pixels[offset] = 0xFF
	frame.pixels[offset+1] = frame.pixels[offset+1] + 2
	frame.pixels[offset+2] = frame.pixels[offset+2] + 2
	frame.pixels[offset+3] = 0xFF
}

var count int
var mtx sync.Mutex

func main() {

}
