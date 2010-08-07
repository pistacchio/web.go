/*
 * note on the Session implementation:
 * at present time, when storing session data to cookies or files
 * all numeric types must be float64
 */
 
package web

import (
  "json"
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

type Session map[string]interface{}

type SessionHandler interface {
  LoadSession(*Context)
  SaveSession(*Context)
  Init() bool
  GetSessionLength() int64
}


/*
 * in-memory sessions
 */
 
type MemorySessionHandler struct {
  Sessions map[string]Session 
  LastAccess map[string]int64 
  SessionLength int64         // in seconds
}

func (this *MemorySessionHandler) LoadSession(ctx *Context) {
  ok := LoadSessionId(ctx, this)
  ctx.Session, ok = this.Sessions[ctx.SessionId]
  
  // initialize an empty session if no previous one is found
  if !ok {
    ctx.Session = make(Session)
  }
  
  // set to "now" the last access for the session
  this.LastAccess[ctx.SessionId] = time.Seconds()
}

func (this *MemorySessionHandler) SaveSession(ctx *Context) {
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

func (this *MemorySessionHandler) GetSessionLength() int64 {
  return this.SessionLength
}

/*
 * cookie-based sessions
 */
 
type CookieSessionHandler struct {
  SessionLength int64         // in seconds
}

func (this *CookieSessionHandler) LoadSession(ctx *Context) {
  LoadSessionId(ctx, this)
  ctx.Session = make(Session)
  
  // session variables stored like key1::value1||key2::value2||key3::value3
  sessionData, ok := ctx.GetSecureCookie("sessionData")
  if ok {
    json.Unmarshal([]byte(sessionData), &ctx.Session)
  }
}

func (this *CookieSessionHandler) SaveSession(ctx *Context) {
  sessionData, _ := json.Marshal(ctx.Session)
  ctx.SetSecureCookie("sessionData", string(sessionData), this.SessionLength)
}

func (this *CookieSessionHandler) Init() bool {
  // set session length in seconds
  length, err := Config.GetInt("session", "length")
  if err != nil {
    this.SessionLength = DefaultSessionLength
  } else {
    this.SessionLength = int64(length)
  }
  
  return true
}

func (this *CookieSessionHandler) GetSessionLength() int64 {
  return this.SessionLength
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
func (this *DummySessionHandler) GetSessionLength() int64 { return 0 }

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
      case "cookie":
        sessionHandler = new(CookieSessionHandler)
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

// return true = already existing SessionId
// false = newly created SessionId
func LoadSessionId(ctx *Context, sessionHandler SessionHandler) bool {
  sessionId, ok := ctx.GetSecureCookie("sessionId")

  // generate and store a random sessionId if not found on cookies
  if !ok {
    sessionId = strconv.Itoa64(rand.Int63())
    ctx.SetSecureCookie("sessionId", sessionId,
                          sessionHandler.GetSessionLength())
    ctx.SessionId = sessionId
    return false
  }

  ctx.SessionId = sessionId  
  return true
}
