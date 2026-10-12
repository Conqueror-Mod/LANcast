// Command lancastd is the LANcast media server.
//
// The program itself lives in internal/serverapp (ADR 0080), so that the
// single LANcast executable can run it too. This entrypoint stays, with the
// icon resource beside it, for as long as LANcast-Server.exe ships.
package main

import "lancast/internal/serverapp"

func main() { serverapp.Main() }
