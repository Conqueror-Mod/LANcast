// Module lancastplugins holds first-party LANcast plugins and the guest SDK.
// It is a separate module from the main lancast module: its packages compile to
// wasm (GOOS=wasip1) and must never be pulled into the host build.
module lancastplugins

go 1.25
