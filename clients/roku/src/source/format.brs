' Words for numbers. Every field read here can be absent (JSON null), so
' nothing assumes one is there.

function ArtOf(it as object, name as string) as dynamic
    if type(it) <> "roAssociativeArray" then return invalid
    a = it.artwork
    if type(a) <> "roAssociativeArray" then return invalid
    return a[name]
end function

function ProgressOf(it as object) as object
    p = it.progress
    if type(p) <> "roAssociativeArray" then return invalid
    return p
end function

function IsWatched(it as object) as boolean
    p = ProgressOf(it)
    return p <> invalid and p.watched = true
end function

' ResumeAt is where playback would pick up, in milliseconds, or 0.
function ResumeAt(it as object) as dynamic
    p = ProgressOf(it)
    if p = invalid or p.watched = true or p.position_ms = invalid then return 0
    return p.position_ms
end function

function DurationText(ms as dynamic) as string
    if ms = invalid or ms <= 0 then return ""
    mins = Int(ms / 60000 + 0.5)
    if mins < 60 then return mins.toStr() + " min"
    h = Int(mins / 60)
    r = mins - h * 60
    if r = 0 then return h.toStr() + " h"
    return h.toStr() + " h " + r.toStr() + " min"
end function

function ClockText(ms as dynamic) as string
    s = Int(ms / 1000)
    h = Int(s / 3600)
    mm = Int((s - h * 3600) / 60)
    ss = s - h * 3600 - mm * 60
    t = Right("0" + mm.toStr(), 2) + ":" + Right("0" + ss.toStr(), 2)
    if h > 0 then t = h.toStr() + ":" + t
    return t
end function

' TileLine is the small line under a poster: the year, and where you are with it.
function TileLine(it as object) as string
    parts = []
    if it.year <> invalid then parts.push(it.year.toStr())
    if it.kind = "show" then
        if it.unwatched_episodes <> invalid and it.unwatched_episodes > 0 then parts.push(it.unwatched_episodes.toStr() + " unwatched")
    else if IsWatched(it) then
        parts.push("Watched")
    else if ResumeAt(it) > 0 then
        parts.push("Started")
    end if
    return parts.Join(" · ")
end function

' MetaLine is the line under a title on its page.
function MetaLine(it as object) as string
    parts = []
    if it.year <> invalid then parts.push(it.year.toStr())
    d = DurationText(it.duration_ms)
    if d <> "" and it.kind <> "show" then parts.push(d)
    if it.content_rating <> invalid and it.content_rating <> "" then parts.push(it.content_rating)
    if it.rating <> invalid and it.rating > 0 then parts.push("Rated " + Str(Int(it.rating * 10 + 0.5) / 10).Trim())
    return parts.Join(" · ")
end function

function GenresLine(it as object) as string
    if type(it.genres) <> "roArray" then return ""
    return it.genres.Join(", ")
end function

' EpisodeLine is the line under an episode's title.
function EpisodeLine(it as object) as string
    parts = []
    if it.episode <> invalid then parts.push("Episode " + it.episode.toStr())
    d = DurationText(it.duration_ms)
    if d <> "" then parts.push(d)
    if IsWatched(it) then
        parts.push("Watched")
    else if ResumeAt(it) > 0 then
        parts.push("Stopped at " + ClockText(ResumeAt(it)))
    end if
    return parts.Join(" · ")
end function
