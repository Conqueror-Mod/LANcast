sub init()
    m.who = m.top.findNode("who")
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.signout = m.top.findNode("signout")
    m.change = m.top.findNode("change")
    m.signout.observeField("selected", "onSignOut")
    m.change.observeField("selected", "onChange")
    m.grid.observeField("itemSelected", "onLibrary")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if not m.top.hasFocus() then return
    if m.grid.content <> invalid and m.grid.content.getChildCount() > 0 then
        m.grid.setFocus(true)
    else
        m.signout.setFocus(true)
    end if
end sub

sub showWho()
    if m.top.userName <> "" then
        m.who.text = "Signed in as " + m.top.userName + " on " + m.top.server
    else
        m.who.text = "Signed in on " + m.top.server
    end if
end sub

sub showLibraries()
    content = CreateObject("roSGNode", "ContentNode")
    for each lib in m.top.libraries
        c = content.createChild("ContentNode")
        c.title = lib.name
        detail = lib.kind
        if lib.item_count <> invalid then detail = detail + " · " + lib.item_count.toStr()
        c.description = detail
    end for
    m.grid.content = content
    if content.getChildCount() = 0 then
        m.status.text = "No libraries you can see on this server yet."
    else
        m.status.text = ""
    end if
    if m.top.hasFocus() or m.top.isInFocusChain() then onFocus()
end sub

sub showMessage()
    m.status.text = m.top.message
end sub

sub onLibrary()
    lib = m.top.libraries[m.grid.itemSelected]
    if lib <> invalid then m.status.text = lib.name + ": browsing comes in the next build."
end sub

sub onSignOut()
    m.top.signOut = true
end sub

sub onChange()
    m.top.changeServer = true
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "down" and m.grid.hasFocus() then
        m.signout.setFocus(true)
        return true
    else if key = "up" and (m.signout.hasFocus() or m.change.hasFocus()) and m.grid.content <> invalid and m.grid.content.getChildCount() > 0 then
        m.grid.setFocus(true)
        return true
    else if key = "right" and m.signout.hasFocus() then
        m.change.setFocus(true)
        return true
    else if key = "left" and m.change.hasFocus() then
        m.signout.setFocus(true)
        return true
    end if
    return false
end function
