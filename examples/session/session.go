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
  var num int = 0
  
  c := ctx.GetSessionItem("num")
  if c != nil {
    num = c.(int)
    num++
    ctx.SetSessionItem("num", num)
  } else {
    ctx.SetSessionItem("num", 0)
  }

  return "You hit this page " + strconv.Itoa(num) + " times"
  
}

func main() {
    web.Get("/", index)
    web.Run(wsHost + ":" + wsPort)
}
