' A film shows Play (and Resume when started); a show lists its seasons and,
' beside them, the episodes of the one you are on. Playing is step 5.

sub init()
    m.backdrop = m.top.findNode("backdrop")
    m.poster = m.top.findNode("poster")
    m.title = m.top.findNode("title")
    m.meta = m.top.findNode("meta")
    m.genres = m.top.findNode("genres")
    m.overview = m.top.findNode("overview")
    m.play = m.top.findNode("play")
    m.restart = m.top.findNode("restart")
    m.seasons = m.top.findNode("seasons")
    m.episodes = m.top.findNode("episodes")
    m.status = m.top.findNode("status")
    m.isShow = false
    m.seasonItems = []
    m.episodeItems = []
    m.shownSeason = -1
    m.synopsis = ""
    m.play.observeField("selected", "onPlay")
    m.restart.observeField("selected", "onPlay")
    m.seasons.observeField("itemFocused", "onSeasonFocused")
    m.seasons.observeField("itemSelected", "onSeasonSelected")
    m.episodes.observeField("itemFocused", "onEpisodeFocused")
    m.episodes.observeField("itemSelected", "onPlay")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if m.top.hasFocus() then focusFirst()
end sub

sub focusFirst()
    if m.isShow then
        if m.episodeItems.count() > 0 then
            m.episodes.setFocus(true)
        else if m.seasonItems.count() > 0 then
            m.seasons.setFocus(true)
        end if
    else
        m.play.setFocus(true)
    end if
end sub

sub load()
    it = m.top.item
    m.isShow = it.kind = "show"
    paint(it)
    m.detailTask = ApiGet("/api/items/" + it.id.toStr(), "onDetail")
    if m.isShow then
        m.status.text = "Loading episodes…"
        m.childTask = ApiGet("/api/items?parent_id=" + it.id.toStr() + "&limit=500", "onChildren")
    else
        m.play.visible = true
        resumeButtons(it)
        if m.top.isInFocusChain() then focusFirst()
    end if
end sub

sub paint(it as object)
    m.title.text = it.title
    m.meta.text = MetaLine(it)
    m.genres.text = GenresLine(it)
    if it.overview <> invalid then m.synopsis = it.overview
    m.overview.text = m.synopsis
    p = ArtworkUrl(ArtOf(it, "poster"), "poster2x")
    if p <> "" then m.poster.uri = p
    f = ArtworkUrl(ArtOf(it, "fanart"), "fanart")
    if f <> "" then m.backdrop.uri = f
end sub

sub resumeButtons(it as object)
    if m.isShow then return
    at = ResumeAt(it)
    if at > 0 then
        m.play.text = "Resume from " + ClockText(at)
        m.restart.visible = true
    else
        m.play.text = "Play"
        m.restart.visible = false
    end if
end sub

sub onDetail(e as object)
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" then return
    paint(r.data)
    resumeButtons(r.data)
end sub

' ---- a show ------------------------------------------------------------

sub onChildren(e as object)
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" or type(r.data.items) <> "roArray" then
        m.status.text = "Could not load the episodes: " + r.error
        return
    end if
    kids = r.data.items
    if kids.count() = 0 then
        m.status.text = "No episodes of this show are here yet."
        return
    end if
    if kids[0].kind = "season" then
        m.seasonItems = kids
        content = CreateObject("roSGNode", "ContentNode")
        start = -1
        for i = 0 to kids.count() - 1
            c = content.createChild("ContentNode")
            c.title = kids[i].title
            c.addFields({ shown: false })
            ' Open on the first season with something left to watch, passing
            ' over specials (season 0).
            n = kids[i].unwatched_episodes
            if start < 0 and n <> invalid and n > 0 and kids[i].season <> 0 then start = i
        end for
        if start < 0 then start = 0
        m.seasons.content = content
        m.seasons.visible = true
        m.seasons.jumpToItem = start
        showSeason(start)
    else
        ' A show whose episodes sit directly under it, with no seasons.
        m.episodes.translation = [96, 600]
        m.episodes.itemSize = [1724, 120]
        showEpisodes(kids)
    end if
end sub

sub onSeasonFocused()
    showSeason(m.seasons.itemFocused)
end sub

sub onSeasonSelected()
    if m.episodeItems.count() > 0 then
        m.episodes.setFocus(true)
        onEpisodeFocused()
    end if
end sub

sub showSeason(i as integer)
    if i < 0 or i >= m.seasonItems.count() then return
    if m.shownSeason = i then return
    m.shownSeason = i
    content = m.seasons.content
    for j = 0 to content.getChildCount() - 1
        content.getChild(j).shown = (j = i)
    end for
    m.status.text = "Loading episodes…"
    m.episodeTask = ApiGet("/api/items?parent_id=" + m.seasonItems[i].id.toStr() + "&limit=500", "onEpisodes")
end sub

sub onEpisodes(e as object)
    ' Moving along the seasons quickly leaves answers behind: only the last counts.
    if not e.getRoSGNode().isSameNode(m.episodeTask) then return
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" or type(r.data.items) <> "roArray" then
        m.status.text = "Could not load the episodes: " + r.error
        return
    end if
    showEpisodes(r.data.items)
end sub

sub showEpisodes(eps as object)
    m.episodeItems = eps
    content = CreateObject("roSGNode", "ContentNode")
    start = -1
    for i = 0 to eps.count() - 1
        ep = eps[i]
        c = content.createChild("ContentNode")
        c.title = ep.title
        c.description = EpisodeLine(ep)
        pic = ArtOf(ep, "thumb")
        if pic = invalid then pic = ArtOf(ep, "poster")
        if pic = invalid then pic = ArtOf(ep, "fanart")
        c.HDPosterUrl = ArtworkUrl(pic, "thumb")
        if start < 0 and not IsWatched(ep) then start = i
    end for
    if start < 0 then start = 0
    m.episodes.content = content
    m.episodes.visible = eps.count() > 0
    if eps.count() > 0 then m.episodes.jumpToItem = start
    if eps.count() = 0 then
        m.status.text = "No episodes in this season are here yet."
    else
        m.status.text = ""
    end if
    ' Opening the page lands on the episodes; moving along the seasons stays there.
    if m.top.isInFocusChain() and not m.seasons.hasFocus() and eps.count() > 0 then
        m.episodes.setFocus(true)
        onEpisodeFocused()
    end if
end sub

sub onEpisodeFocused()
    i = m.episodes.itemFocused
    if not m.episodes.hasFocus() or i < 0 or i >= m.episodeItems.count() then return
    ep = m.episodeItems[i]
    if ep.overview <> invalid and ep.overview <> "" then
        m.overview.text = ep.overview
    else
        m.overview.text = m.synopsis
    end if
end sub

' ---- both --------------------------------------------------------------

sub onPlay()
    m.status.text = "Playing comes in the next build."
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if m.isShow then
        if key = "right" and m.seasons.hasFocus() and m.episodeItems.count() > 0 then
            m.episodes.setFocus(true)
            onEpisodeFocused()
            return true
        else if key = "left" and m.episodes.hasFocus() and m.seasons.visible then
            m.seasons.setFocus(true)
            m.overview.text = m.synopsis
            return true
        end if
    else
        if key = "right" and m.play.hasFocus() and m.restart.visible then
            m.restart.setFocus(true)
            return true
        else if key = "left" and m.restart.hasFocus() then
            m.play.setFocus(true)
            return true
        end if
    end if
    return false
end function
