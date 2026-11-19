' The flow: an address once, then a code to pair, then home. What the TV
' remembers is in store.brs; what each screen shows is its own component.

sub init()
    m.holder = m.top.findNode("holder")
    m.check = PairingSelfCheck()
    print "LANcast pairing self-check: "; m.check.ok; " "; m.check.code; " "; m.check.digest
    route()
end sub

sub route()
    s = StoreRead()
    if s.server = "" then
        showAddress("", "")
    else if s.key = "" then
        startPair("")
    else
        showHome()
    end if
end sub

sub swap(screen as object)
    stopPairing()
    m.holder.removeChildrenIndex(m.holder.getChildCount(), 0)
    m.holder.appendChild(screen)
    m.screen = screen
    screen.setFocus(true)
end sub

' ---- address -----------------------------------------------------------

sub showAddress(typed as string, message as string)
    s = CreateObject("roSGNode", "AddressScreen")
    s.server = typed
    s.message = message
    s.observeField("submitted", "onAddress")
    swap(s)
end sub

sub onAddress()
    typed = m.screen.submitted
    base = NormalizeAddress(typed)
    if base = "" then return
    m.screen.busy = true
    m.connect = CreateObject("roSGNode", "ConnectTask")
    m.connect.base = base
    m.connect.observeField("result", "onConnected")
    m.connect.control = "run"
    m.connecting = { typed: typed, base: base }
end sub

sub onConnected()
    r = m.connect.result
    if not r.ok then
        m.screen.busy = false
        m.screen.message = r.error
        return
    end if
    StoreForgetAll()
    publicFlag = "0"
    if r.public then publicFlag = "1"
    StoreWrite({ server: m.connecting.base, public: publicFlag, pem: r.pem })
    startPair("")
end sub

' ---- pairing -----------------------------------------------------------

sub startPair(note as string)
    s = StoreRead()
    screen = CreateObject("roSGNode", "PairScreen")
    screen.server = s.server
    screen.observeField("newCode", "onNewCode")
    screen.observeField("changeServer", "onChangeServer")
    swap(screen)
    if not m.check.ok then
        screen.message = "This TV works the pairing code out differently from the server (self-check " + m.check.code + "), so the code may not be found. Please report it."
    else if note <> "" then
        screen.message = note
    else
        screen.message = "Asking the server for a code…"
    end if

    t = CreateObject("roSGNode", "PairTask")
    t.base = s.server
    t.public = s.public = "1"
    t.pem = s.pem
    t.name = deviceName()
    t.observeField("code", "onPairCode")
    t.observeField("state", "onPairState")
    t.observeField("message", "onPairMessage")
    t.control = "run"
    m.pair = t
end sub

function deviceName() as string
    di = CreateObject("roDeviceInfo")
    name = di.GetFriendlyName()
    if name = invalid or name.Trim() = "" then name = di.GetModelDisplayName()
    if name = invalid or name.Trim() = "" then name = "Roku"
    return Left(name.Trim(), 64)
end function

sub stopPairing()
    if m.pair <> invalid then
        m.pair.cancel = true
        m.pair = invalid
    end if
end sub

sub onPairCode()
    if m.pair = invalid then return
    m.screen.code = m.pair.code
    if m.check.ok then m.screen.message = "Waiting for somebody to approve this TV…"
end sub

sub onPairMessage()
    if m.pair <> invalid and m.pair.message <> "" then m.screen.message = m.pair.message
end sub

sub onPairState()
    t = m.pair
    if t = invalid then return
    if t.state = "approved" then
        r = t.result
        StoreWrite({ key: r.key, keyId: r.keyId, userName: r.userName })
        m.pair = invalid
        showHome()
    else if t.state = "refused" or t.state = "error" then
        m.screen.code = ""
        m.screen.message = t.message + " Choose Get a new code to try again."
        m.pair = invalid
    end if
end sub

sub onNewCode()
    startPair("")
end sub

sub onChangeServer()
    s = StoreRead()
    StoreForgetAll()
    showAddress(s.server, "")
end sub

' ---- home --------------------------------------------------------------

sub showHome()
    s = StoreRead()
    screen = CreateObject("roSGNode", "HomeScreen")
    screen.server = s.server
    screen.userName = s.userName
    screen.observeField("signOut", "onSignOut")
    screen.observeField("changeServer", "onChangeServer")
    swap(screen)

    t = CreateObject("roSGNode", "ApiTask")
    t.url = s.server + "/api/libraries"
    t.public = s.public = "1"
    if s.public <> "1" then t.pemPath = PinPath(s.pem)
    t.key = s.key
    t.observeField("response", "onLibraries")
    t.control = "run"
    m.api = t
end sub

sub onLibraries()
    r = m.api.response
    if r.code = 200 and r.data <> invalid then
        m.screen.libraries = r.data
    else if r.code = 401 then
        ' The key was revoked in Settings: this TV is signed out.
        StoreForgetKey()
        startPair("This TV was signed out in LANcast. Pair it again to carry on.")
    else if r.code <= 0 and certificateFailure(r.error) then
        ' ADR 0125: a changed serving certificate means pairing again.
        StoreForgetKey()
        startPair("The server's certificate has changed since this TV was paired, so it must be paired again.")
    else
        m.screen.message = "Could not load your libraries: " + r.error
    end if
end sub

function certificateFailure(e as string) as boolean
    l = LCase(e)
    return Instr(1, l, "ssl") > 0 or Instr(1, l, "certificate") > 0
end function

sub onSignOut()
    StoreForgetKey()
    startPair("Signed out. The key stays listed in LANcast's Settings until you revoke it there.")
end sub
