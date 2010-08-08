/*
 * note on the Session implementation:
 * at present time, when storing session data to cookies or files
 * all numeric types must be float64 and no pointer types can be saved
 */
 
package web

import (
  "fmt"
  "io/ioutil"
  "json"
  "os"
  "rand"
  "strconv"
  "time"
)

const (
  DefaultSessionLength  = 600         // 600 seconds = 10 minutes
  SessionCleanerTick    = 60000000000 // 60000000000 nanoseconds = 1 minute
  SessionDirectory      = "session"   // used to store session data when using
                                      //    file-based sessions
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
  SetSessionLength(int64)
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
  ok := LoadSessionId(ctx)
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
  SetSessionLength()
  
  // starts a timer that thicks every n seconds
  // the cleaning goroutine with perform pruning of unused sessions
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

func (this *MemorySessionHandler) SetSessionLength(length int64) {
  this.SessionLength = length
}

/*
 * cookie-based sessions
 */
 
type CookieSessionHandler struct {
  SessionLength int64         // in seconds
}

func (this *CookieSessionHandler) LoadSession(ctx *Context) {
  LoadSessionId(ctx)
  ctx.Session = make(Session)
  
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
  SetSessionLength()
  
  return true
}

func (this *CookieSessionHandler) GetSessionLength() int64 {
  return this.SessionLength
}

func (this *CookieSessionHandler) SetSessionLength(length int64) {
  this.SessionLength = length
}

/*
 * file-based sessions
 */

type FileSessionHandler struct {
  SessionLength int64         // in seconds
}

func (this *FileSessionHandler) LoadSession(ctx *Context) {
  LoadSessionId(ctx)
  ctx.Session = make(Session)  
  sessionFile := fmt.Sprintf("%s/%s", SessionDirectory, ctx.SessionId)

  // if the file is not found, just touch it
  ok := fileExists(sessionFile)
  if !ok {
    ioutil.WriteFile(sessionFile, make([]byte, 0), 0660)
    return
  }

  sessionData, err := ioutil.ReadFile(sessionFile)
  if err == nil {
    json.Unmarshal(sessionData, &ctx.Session)
  }
}

func (this *FileSessionHandler) SaveSession(ctx *Context) {
  sessionFile := fmt.Sprintf("%s/%s", SessionDirectory, ctx.SessionId)
  sessionData, _ := json.Marshal(ctx.Session)
  ioutil.WriteFile(sessionFile, sessionData, 660)
}

func (this *FileSessionHandler) Init() bool {
  SetSessionLength()

 // check if "session" directory exists
 if !dirExists(SessionDirectory) {
   fmt.Printf("To use file-based sessions, please create a \"%s\" dir\n", 
                  SessionDirectory)
   return false
 }

 // starts a timer that thicks every n seconds
 // the cleaning goroutine with perform pruning of unused session files
 // every tick
 SessionCleanerTimer := time.NewTicker(SessionCleanerTick)
 
  go func() {
    for {
      sessionDirFiles, _ := ioutil.ReadDir(SessionDirectory)
      for _, file := range sessionDirFiles {
        // delete the file if too old
        if file.Mtime_ns + this.SessionLength > time.Seconds() {
          sessionFile := fmt.Sprintf("%s/%s", SessionDirectory, file.Name)
          os.Remove(sessionFile)
        }
      }
      <- SessionCleanerTimer.C
    }
  }()


 return true
}

func (this *FileSessionHandler) GetSessionLength() int64 {
  return this.SessionLength
}

func (this *FileSessionHandler) SetSessionLength(length int64) {
  this.SessionLength = length
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
func (this *DummySessionHandler) SetSessionLength(length int64) { }

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
      case "file":
        sessionHandler = new(FileSessionHandler)
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

func SetSessionLength() {
  // set session length in seconds
  length, err := Config.GetInt("session", "length")
  if err != nil {
    sessionHandler.SetSessionLength(DefaultSessionLength)
  } else {
    sessionHandler.SetSessionLength(int64(length))
  }
  
}

// return true = already existing SessionId
// false = newly created SessionId
func LoadSessionId(ctx *Context) bool {
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
