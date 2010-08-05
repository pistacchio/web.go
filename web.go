package web

import (
    "bytes"
    "container/vector"
    "crypto/hmac"
    "encoding/base64"
    "fmt"
    "http"
    "io/ioutil"
    "log"
    "os"
    "path"
    "reflect"
    "regexp"
    "strconv"
    "strings"
    "time"
    "goconf.googlecode.com/hg"
    "rand"
)

func init() {
    contextType = reflect.Typeof(Context{})
    //find the location of the exe file
    arg0 := path.Clean(os.Args[0])
    wd, _ := os.Getwd()
    var exeFile string
    if strings.HasPrefix(arg0, "/") {
        exeFile = arg0
    } else {
        //TODO for robustness, search each directory in $PATH
        exeFile = path.Join(wd, arg0)
    }
    root, _ := path.Split(exeFile)
    staticDir = path.Join(root, "static")
    
    // configuration
    configFile := path.Join(root, "webgo.config")
    Config, _ = conf.ReadConfigFile(configFile)
    
    // cookie security configuration
    cookieSecretSalt, err := Config.GetString("security", "cookieSecretSalt")
    if err == nil {
      SetCookieSecret(cookieSecretSalt)
    }
    
    SessionHandler = new(MemorySessionHandler)
    SessionHandler.Init()
}

const (
  defaultSessionDuration = 600 // 10 minutes in seconds
  sessioCleanerTick = 60000000000 // 1 minute in nanoseconds
)

var (
  //secret key used to store cookies
  secret = ""

  contextType reflect.Type
  staticDir string
  
  Config *conf.ConfigFile  
  routes vector.Vector
  SessionHandler sessionHandler
)

type conn interface {
    StartResponse(status int)
    SetHeader(hdr string, val string, unique bool)
    Write(data []byte) (n int, err os.Error)
    Close()
}

/*
 * Secret cookies
 */

func SetCookieSecret(key string) {
  secret = key
}

func getCookieSig(val []byte, timestamp string) string {
    hm := hmac.NewSHA1([]byte(secret))

    hm.Write(val)
    hm.Write([]byte(timestamp))

    hex := fmt.Sprintf("%02x", hm.Sum())
    return hex
}

/*
 * Context
 */

type Context struct {
    *Request
    *conn
    Session
    SessionId string
    responseStarted bool
}

func (ctx *Context) StartResponse(status int) {
    ctx.conn.StartResponse(status)
    ctx.responseStarted = true
}

func (ctx *Context) Write(data []byte) (n int, err os.Error) {
    if !ctx.responseStarted {
        ctx.StartResponse(200)
    }

    //if it's a HEAD request, we just write blank data
    if ctx.Request.Method == "HEAD" {
        data = []byte{}
    }

    return ctx.conn.Write(data)
}
func (ctx *Context) WriteString(content string) {
    ctx.Write([]byte(content))
}

func (ctx *Context) Abort(status int, body string) {
    ctx.StartResponse(status)
    ctx.WriteString(body)
}

func (ctx *Context) Redirect(status int, url string) {
    ctx.SetHeader("Location", url, true)
    ctx.StartResponse(status)
    ctx.WriteString("Redirecting to: " + url)
}

func (ctx *Context) NotFound(message string) {
    ctx.StartResponse(404)
    ctx.WriteString(message)
}

/*
 * Cookies
 */

//Sets a cookie -- duration is the amount of time in seconds. 0 = forever
func (ctx *Context) SetCookie(name string, value string, age int64) {
    if age == 0 {
        //do some really long time
    }

    utctime := time.UTC()
    utc1 := time.SecondsToUTC(utctime.Seconds() + 60*30)
    cookie := fmt.Sprintf("%s=%s; expires=%s", name, value, webTime(utc1))
    ctx.SetHeader("Set-Cookie", cookie, false)
}

func (ctx *Context) SetSecureCookie(name string, val string, age int64) {
    //base64 encode the val
    if len(secret) == 0 {
        log.Stderrf("Secret Key for secure cookies has not been set. Please call web.SetCookieSecret\n")
        return
    }
    var buf bytes.Buffer
    encoder := base64.NewEncoder(base64.StdEncoding, &buf)
    encoder.Write([]byte(val))
    encoder.Close()
    vs := buf.String()
    vb := buf.Bytes()

    timestamp := strconv.Itoa64(time.Seconds())

    sig := getCookieSig(vb, timestamp)

    cookie := strings.Join([]string{vs, timestamp, sig}, "|")

    ctx.SetCookie(name, cookie, age)
}

func (ctx *Context) GetSecureCookie(name string) (string, bool) {

    cookie, ok := ctx.Request.Cookies[name]

    if !ok {
        return "", false
    }

    parts := strings.Split(cookie, "|", 3)

    val := parts[0]
    timestamp := parts[1]
    sig := parts[2]

    if getCookieSig([]byte(val), timestamp) != sig {
        return "", false
    }

    ts, _ := strconv.Atoi64(timestamp)

    if time.Seconds()-31*86400 > ts {
        return "", false
    }

    buf := bytes.NewBufferString(val)
    encoder := base64.NewDecoder(base64.StdEncoding, buf)

    res, _ := ioutil.ReadAll(encoder)
    return string(res), true
}

/*
 * route
 */

type route struct {
    r       string
    cr      *regexp.Regexp
    method  string
    handler *reflect.FuncValue
}

func addRoute(r string, method string, handler interface{}) {
    cr, err := regexp.Compile(r)
    if err != nil {
        log.Stderrf("Error in route regex %q\n", r)
        return
    }
    fv := reflect.NewValue(handler).(*reflect.FuncValue)
    routes.Push(route{r, cr, method, fv})
}

/*
 * httpConn
 */

type httpConn struct {
    conn *http.Conn
}

func (c *httpConn) StartResponse(status int) { c.conn.WriteHeader(status) }

func (c *httpConn) SetHeader(hdr string, val string, unique bool) {
    //right now unique can't be implemented through the http package.
    //see issue 488
    c.conn.SetHeader(hdr, val)
}

func (c *httpConn) WriteString(content string) {
    buf := bytes.NewBufferString(content)
    c.conn.Write(buf.Bytes())
}

func (c *httpConn) Write(content []byte) (n int, err os.Error) {
    return c.conn.Write(content)
}

func (c *httpConn) Close() {
    rwc, buf, _ := c.conn.Hijack()
    if buf != nil {
        buf.Flush()
    }

    if rwc != nil {
        rwc.Close()
    }
}

func httpHandler(c *http.Conn, req *http.Request) {
    conn := httpConn{c}
    wreq := newRequest(req)
    routeHandler(wreq, &conn)
}

func routeHandler(req *Request, c conn) {
    requestPath := req.URL.Path

    //log the request
    if len(req.URL.RawQuery) == 0 {
        log.Stdout(req.Method + " " + requestPath)
    } else {
        log.Stdout(requestPath + "?" + req.URL.RawQuery)
    }

    //parse the form data (if it exists)
    perr := req.parseParams()
    if perr != nil {
        log.Stderrf("Failed to parse form data %q", perr.String())
    }

    //parse the cookies
    perr = req.parseCookies()
    if perr != nil {
        log.Stderrf("Failed to parse cookies %q", perr.String())
    }

    ctx := *new(Context)
    ctx.Request = req
    ctx.conn = &c
    ctx.responseStarted = false

    //set some default headers
    ctx.SetHeader("Content-Type", "text/html; charset=utf-8", true)
    ctx.SetHeader("Server", "web.go", true)

    tm := time.LocalTime()
    ctx.SetHeader("Date", webTime(tm), true)

    //try to serve a static file
    staticFile := path.Join(staticDir, requestPath)
    if fileExists(staticFile) && (req.Method == "GET" || req.Method == "HEAD") {
        serveFile(&ctx, staticFile)
        return
    }

    for i := 0; i < routes.Len(); i++ {
        route := routes.At(i).(route)
        cr := route.cr
        //if the methods don't match, skip this handler (except HEAD can be used in place of GET)
        if req.Method != route.method && !(req.Method == "HEAD" && route.method == "GET") {
            continue
        }

        if !cr.MatchString(requestPath) {
            continue
        }
        match := cr.MatchStrings(requestPath)

        if len(match[0]) != len(requestPath) {
            continue
        }

        var args vector.Vector

        handlerType := route.handler.Type().(*reflect.FuncType)

        //check if the first arg in the handler is a context type
        if handlerType.NumIn() > 0 {
            if a0, ok := handlerType.In(0).(*reflect.PtrType); ok {
                typ := a0.Elem()
                if typ == contextType {
                    args.Push(reflect.NewValue(&ctx))
                }
            }
        }

        for _, arg := range match[1:] {
            args.Push(reflect.NewValue(arg))
        }

        if args.Len() != handlerType.NumIn() {
            log.Stderrf("Incorrect number of arguments for %s\n", requestPath)
            ctx.Abort(500, "Server Error")
            return
        }

        valArgs := make([]reflect.Value, args.Len())
        for i := 0; i < args.Len(); i++ {
            valArgs[i] = args.At(i).(reflect.Value)
        }

        SessionHandler.ParseSession(&ctx)
        ret := route.handler.Call(valArgs)
        SessionHandler.StoreSession(&ctx)

        if len(ret) == 0 {
            return
        }
        
        sval, ok := ret[0].(*reflect.StringValue)

        if ok && !ctx.responseStarted {
            content := []byte(sval.Get())
            ctx.SetHeader("Content-Length", strconv.Itoa(len(content)), true)
            ctx.StartResponse(200)
            ctx.Write(content)
        }

        return
    }

    //try to serve index.html
    if indexPath := path.Join(staticDir, "index.html"); requestPath == "/" && fileExists(indexPath) {
        serveFile(&ctx, indexPath)
        return
    }

    ctx.Abort(404, "Page not found")
}

/*
 * Sessions
 */
 
type sessionHandler interface {
  ParseSession(*Context) (os.Error)
  StoreSession(*Context) (os.Error)
  Init() (os.Error)
}

type Session map[string]interface{}

type MemorySessionHandler struct {
  Sessions map[string]Session
  LastAccess map[string]int64
  Duration int64
}

func (s *MemorySessionHandler) ParseSession(ctx *Context) (os.Error) {
  var sessionId string
  
  // generate a unique sessionId if not found on cookies
  sessionId, ok := ctx.GetSecureCookie("sessionId")
  if !ok {
    sessionId = strconv.Itoa64(rand.Int63())
    ctx.SetSecureCookie("sessionId", sessionId, 0)
    ctx.SessionId = sessionId
    ctx.Session = make(map[string]interface{})
    return nil
  }

  ctx.SessionId = sessionId  
  ctx.Session, ok = s.Sessions[sessionId]
  if !ok {
    ctx.Session = make(map[string]interface{})
  }
  s.LastAccess[sessionId] = time.Seconds()

  return nil
}

func (s *MemorySessionHandler) StoreSession(ctx *Context) (os.Error) {
  sessionId := ctx.SessionId
  s.Sessions[sessionId] = ctx.Session

  return nil
}

func (s *MemorySessionHandler) Init() (os.Error) {
  s.Sessions = make(map[string]Session)
  s.LastAccess = make(map[string]int64)
  
  // set session duration in minutes
  d, err := Config.GetInt("sessions", "duration")
  if err != nil {
    s.Duration = defaultSessionDuration
  } else {
    s.Duration = int64(d) * 60
  }
  
  // start session cleanier
  SessionCleanerTime := time.NewTicker(sessioCleanerTick)
  
  go func() {
    for {
      for sessionId, access := range s.LastAccess {
          if access + s.Duration > time.Seconds() {
            s.Sessions[sessionId] = nil, false
            s.LastAccess[sessionId] = 0, false
          }
      }
      <- SessionCleanerTime.C
    }
  }()

  return nil
}

/*
 * Server
 */

//runs the web application and serves http requests
func Run(addr string) {
    http.Handle("/", http.HandlerFunc(httpHandler))

    log.Stdoutf("web.go serving %s", addr)
    err := http.ListenAndServe(addr, nil)
    if err != nil {
        log.Exit("ListenAndServe:", err)
    }
}

//runs the web application and serves scgi requests
func RunScgi(addr string) {
    log.Stdoutf("web.go serving scgi %s", addr)
    listenAndServeScgi(addr)
}

//runs the web application by serving fastcgi requests
func RunFcgi(addr string) {
    log.Stdoutf("web.go serving fcgi %s", addr)
    listenAndServeFcgi(addr)
}

//Adds a handler for the 'GET' http method.
func Get(route string, handler interface{}) { addRoute(route, "GET", handler) }

//Adds a handler for the 'POST' http method.
func Post(route string, handler interface{}) { addRoute(route, "POST", handler) }

//Adds a handler for the 'PUT' http method.
func Put(route string, handler interface{}) { addRoute(route, "PUT", handler) }

//Adds a handler for the 'DELETE' http method.
func Delete(route string, handler interface{}) {
    addRoute(route, "DELETE", handler)
}

func webTime(t *time.Time) string {
    ftime := t.Format(time.RFC1123)
    if strings.HasSuffix(ftime, "UTC") {
        ftime = ftime[0:len(ftime)-3] + "GMT"
    }
    return ftime
}

func dirExists(dir string) bool {
    d, e := os.Stat(dir)
    switch {
    case e != nil:
        return false
    case !d.IsDirectory():
        return false
    }

    return true
}

func fileExists(dir string) bool {
    info, err := os.Stat(dir)
    if err != nil {
        return false
    } else if !info.IsRegular() {
        return false
    }

    return true
}

//changes the location of the static directory. by default, it's under the 'static' folder
//of the directory containing the web application
func SetStaticDir(dir string) os.Error {
    if !dirExists(dir) {
        msg := fmt.Sprintf("Failed to set static directory %q - does not exist", dir)
        return os.NewError(msg)
    }
    staticDir = dir

    return nil
}

func Urlencode(data map[string]string) string {
    var buf bytes.Buffer
    for k, v := range data {
        buf.WriteString(http.URLEscape(k))
        buf.WriteByte('=')
        buf.WriteString(http.URLEscape(v))
        buf.WriteByte('&')
    }
    s := buf.String()
    return s[0 : len(s)-1]
}
