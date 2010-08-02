package main

import (
    "web"
)

var (
  wsHost, _ = web.Config.GetString("webserver", "host") 
  wsPort, _ = web.Config.GetString("webserver", "port")
)


func index() string { return "OK " }

func main() {
    web.Get("/", index)
    web.Run(wsHost + ":" + wsPort)
}
