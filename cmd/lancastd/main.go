// Command lancastd is the LANcast server alone, for development.
//
// Releases ship one executable, LANcast.exe, built from cmd/lancast, which is
// the app and, by subcommand, this same server (ADR 0080). This entrypoint
// stays because a server-only build is handy when working on the server, and
// it costs three lines: the program itself lives in internal/serverapp.
package main

import "lancast/internal/serverapp"

func main() { serverapp.Main() }
