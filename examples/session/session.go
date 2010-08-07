package main

import (
    "web"
    "strconv"
)

var (
  wsHost, _ = web.Config.GetString("webserver", "host") 
  wsPort, _ = web.Config.GetString("webserver", "port")
)


func index(ctx *web.Context) string {
  var num float64 = 0

  c, ok := ctx.Session["num"]
  if ok {
    num = c.(float64)
    num++
    ctx.Session["num"] = num
  } else {
    ctx.Session["num"] = 0
  }

  return "You hit this page " + strconv.Ftoa64(num, 'f', 0) + " times"
  
  return "oK"
}

func main() {
    web.Get("/", index)
    web.Run(wsHost + ":" + wsPort)
}
