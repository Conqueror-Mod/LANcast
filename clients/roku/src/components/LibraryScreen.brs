' One page is asked for at a time, and the next when focus nears the end of
' what has arrived, so a library of thousands opens as fast as one of ten.

sub init()
    m.title = m.top.findNode("title")
    m.count = m.top.findNode("count")
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.items = []
    m.total = -1
    m.loading = false
    m.grid.observeField("itemFocused", "onMoved")
    m.grid.observeField("itemSelected", "onChosen")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if m.top.hasFocus() and m.items.count() > 0 then m.grid.setFocus(true)
end sub

sub load()
    lib = m.top.library
    m.title.text = lib.name
    m.content = CreateObject("roSGNode", "ContentNode")
    m.grid.content = m.content
    if lib.kind <> "movie" and lib.kind <> "show" then
        m.status.text = "This TV opens film and show libraries for now. " + lib.name + " is in LANcast on a computer or phone."
        return
    end if
    m.status.text = "Loading…"
    more()
end sub

sub more()
    if m.loading then return
    if m.total >= 0 and m.items.count() >= m.total then return
    m.loading = true
    path = "/api/items?library_id=" + m.top.library.id.toStr() + "&exclude_kind=collection,playlist&limit=100&offset=" + m.items.count().toStr()
    m.task = ApiGet(path, "onPage")
end sub

sub onPage(e as object)
    m.loading = false
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" then
        m.status.text = "Could not load " + m.top.library.name + ": " + r.error
        return
    end if
    first = m.items.count() = 0
    if r.data.total <> invalid then m.total = r.data.total
    if type(r.data.items) = "roArray" then
        for each it in r.data.items
            m.items.push(it)
            c = m.content.createChild("ContentNode")
            c.title = it.title
            c.description = TileLine(it)
            c.HDPosterUrl = ArtworkUrl(ArtOf(it, "poster"), "poster")
        end for
    end if
    if m.total < 0 then m.total = m.items.count()
    if m.items.count() = 0 then
        m.status.text = "Nothing in this library yet."
    else
        m.status.text = ""
    end if
    noun = "films"
    if m.top.library.kind = "show" then noun = "shows"
    if m.total = 1 then noun = Left(noun, Len(noun) - 1)
    m.count.text = m.total.toStr() + " " + noun
    if first and m.items.count() > 0 and (m.top.hasFocus() or m.top.isInFocusChain()) then m.grid.setFocus(true)
end sub

sub onMoved()
    if m.grid.itemFocused >= m.items.count() - 21 then more()
end sub

sub onChosen()
    it = m.items[m.grid.itemSelected]
    if it <> invalid then m.top.open = it
end sub
