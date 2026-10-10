# GeoNames — the gazetteer behind photo places

Photo places ([ADR 0078](../../docs/adr/0078-a-place-is-the-town-a-photo-was-taken-in.md))
name the town a photograph was taken in without asking anybody's server, so
the list of towns is built into the binary: `internal/geo/*.tsv.gz`.

## What ships

| | |
|---|---|
| Source | GeoNames, <https://www.geonames.org/> |
| Licence | **Creative Commons Attribution 4.0** (CC BY 4.0) |
| Files | `places.tsv.gz` (161,617 towns), `regions.tsv.gz`, `countries.tsv.gz` |
| Size | about 3.1 MB, compressed, in `LANcast-Server.exe` |

Attribution: *Place names from GeoNames (geonames.org), licensed under CC BY 4.0.
Trimmed to the fields LANcast uses.*

## What it was built from

Downloaded from <https://download.geonames.org/export/dump/> on 2026-10-09
(the server's Last-Modified was 2026-10-09).

| File | SHA-256 |
|---|---|
| `cities1000.zip` | `81dc87b0348cae3fdcd8cf7b7edad32f21d485ed71a8c54f4886c453dde67173` |
| `admin1CodesASCII.txt` | `1da92a6323a5fec3176f3f743bf4cf4040fd56a876da55e46fbca23c863aa60a` |
| `countryInfo.txt` | `f835fda64430210e0a6a4e1fba9f9e99ed3aa0694e61aeff43fb761ac2613a0f` |

## How it was changed

[`internal/geo/gen/main.go`](../../internal/geo/gen/main.go) keeps seven
columns of `cities1000.txt` (id, name, country, first-level region, latitude
and longitude to four decimals, population) and drops neighbourhoods and
places that no longer exist (`PPLX`, `PPLH`, `PPLQ`, `PPLW`, `PPLCH`). Only
the regions and countries those towns use are kept.

The output is reproducible: rows are sorted and the gzip header carries no name
or time, so unzipping `cities1000.zip` and running

```
go run ./internal/geo/gen/main.go -cities cities1000.txt \
    -admin1 admin1CodesASCII.txt -countries countryInfo.txt -out internal/geo
```

gives the committed files byte for byte:

| File | SHA-256 |
|---|---|
| `places.tsv.gz` | `6ad4720b2ae04f2c2f581f42106d0f0b8665fe5071060e3e7296db3c9c1494de` |
| `regions.tsv.gz` | `2e5e7aef17dab0c13acd8f56e6793d7068bd011424c155720fa7fa81b01b2e1d` |
| `countries.tsv.gz` | `37d7f559027d89eba479e1d859ca6b27de2b56eb23b55a787a4bd94ea816d9a1` |

## Updating it

Nothing fetches it at run time, and nothing needs to: towns move rarely, and a
newer export changes a name here and there. Rebuild from a fresh download when
there is a reason to, record the new hashes above, and expect photos already
placed to keep their town until their location is read again.
