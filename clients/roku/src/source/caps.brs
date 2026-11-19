'' What this TV decodes beyond the server's roku floor, as ?can= claims
' (docs/api.md). Asked of the device, never assumed: a Roku that cannot play
' HEVC gets it converted rather than a black screen. Matroska is not claimed
' until a direct-played MKV has been heard with sound (roku-plan.md, 0a).
function DeviceClaims() as string
    di = CreateObject("roDeviceInfo")
    claims = []
    if di.CanDecodeVideo({ Codec: "hevc", Profile: "main", Level: "5.1" }).Result = true then
        claims.push("hevc")
        if di.CanDecodeVideo({ Codec: "hevc", Profile: "main 10", Level: "5.1" }).Result = true then claims.push("hevc10")
    end if
    return claims.Join(",")
end function
