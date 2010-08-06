package web

import (
  "rand"
  "strconv"
  "time"
)

const (
  DefaultSessionLength  = 600         // 600 seconds = 10 minutes
  SessionCleanerTick    = 60000000000 // 60000000000 nanoseconds = 1 minute
)

var (
  sessionHandler SessionHandler
)

type SessionHandler interface {
  LoadSession(*Context)
  SaveSession(*Context)
  Init() bool
}

type Session map[string]interface{}

/*
 * in-memory sessions
 */
 
type MemorySessionHandler struct {
  Sessions map[string]Session 
  LastAccess map[string]int64 
  SessionLength int64         // in seconds
}

func (this *MemorySessionHandler) LoadSession(ctx *Context) {
  if sessionHandler == nil {
    return
  }

  var sessionId string
    
  sessionId, ok := ctx.GetSecureCookie("sessionId")

  // generate and store a random sessionId if not found on cookies
  if !ok {
    sessionId = strconv.Itoa64(rand.Int63())
    ctx.SetSecureCookie("sessionId", sessionId, this.SessionLength)
    ctx.SessionId = sessionId
    ctx.Session = make(map[string]interface{})
    return
  }

  ctx.SessionId = sessionId  
  ctx.Session, ok = this.Sessions[sessionId]
  
  // initialize an empty session if no previous one is found
  if !ok {
    ctx.Session = make(map[string]interface{})
  }
  
  // set to "now" the last access for the session
  this.LastAccess[sessionId] = time.Seconds()
}

func (this *MemorySessionHandler) SaveSession(ctx *Context) {
  if sessionHandler == nil {
    return
  }

  sessionId := ctx.SessionId
  
  // saves in memory all the changes made to ctx.Session
  this.Sessions[sessionId] = ctx.Session
}

func (this *MemorySessionHandler) Init() bool {
  this.Sessions = make(map[string]Session)
  this.LastAccess = make(map[string]int64)
  
  // set session length in seconds
  length, err := Config.GetInt("session", "length")
  if err != nil {
    this.SessionLength = DefaultSessionLength
  } else {
    this.SessionLength = int64(length)
  }
  
  // starts a timer that thicks every n seconds
  // the cleaning gorouting with perform pruning of unused sessions
  // every tick
  SessionCleanerTimer := time.NewTicker(SessionCleanerTick)
  
  go func() {
    for {
      for sessionId, lastAccessTime := range this.LastAccess {
          // clear the session if expired
          if lastAccessTime + this.SessionLength > time.Seconds() {
            this.Sessions[sessionId] = nil, false
            this.LastAccess[sessionId] = 0, false
          }
      }
      <- SessionCleanerTimer.C
    }
  }()

  return true
}

/*
 * dummy session handler
 */

type DummySessionHandler struct {}

func (this *DummySessionHandler) SaveSession(ctx *Context) {}
func (this *DummySessionHandler) LoadSession(ctx *Context) {
  ctx.Session = make(map[string]interface{})
}
func (this *DummySessionHandler) Init() bool { return true }

/*
 * global functions
 */

func InitSessionHandler() {
  storeType, err := Config.GetString("session", "store")
  var ok bool

  if err == nil {
    switch storeType {
      case "memory":
        sessionHandler = new(MemorySessionHandler)
    }
  }
  
  if sessionHandler != nil {
    ok = sessionHandler.Init()
  } else {
    ok = false
  }
      
  // if no SessionHandler can be created, make a dummy one to allow the call
  // of sessionHandler.LoadSession and sessionHandler.SaveSession
  if !ok {
    sessionHandler = new(DummySessionHandler)
  }

}
