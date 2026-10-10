//go:build ignore

// Builds the gazetteer internal/geo embeds, from GeoNames' own exports.
//
//	go run ./internal/geo/gen/main.go \
//	    -cities cities1000.txt -admin1 admin1CodesASCII.txt \
//	    -countries countryInfo.txt -out internal/geo
//
// The inputs are https://download.geonames.org/export/dump/ (CC BY 4.0); the
// copy the committed tables were built from is recorded, by SHA-256, in
// third_party/geonames/PROVENANCE.md. Running this again over those files
// reproduces the tables byte for byte: rows are sorted, and gzip is written
// with no name and no timestamp.
package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Populated places that are not a place a photograph can be "in".
//
// PPLX is a section of a city — a neighbourhood — and keeping it would split
// one town into a dozen districts. The rest no longer exist: historical,
// abandoned, destroyed, a former capital.
var skip = map[string]bool{
	"PPLX": true, "PPLH": true, "PPLQ": true, "PPLW": true, "PPLCH": true,
}

func main() {
	cities := flag.String("cities", "", "cities1000.txt")
	admin1 := flag.String("admin1", "", "admin1CodesASCII.txt")
	countries := flag.String("countries", "", "countryInfo.txt")
	out := flag.String("out", "", "directory to write the tables into")
	flag.Parse()
	if *cities == "" || *admin1 == "" || *countries == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	var places []string
	admin1Used := map[string]bool{}
	countryUsed := map[string]bool{}
	eachRow(*cities, func(f []string) {
		if len(f) < 15 || skip[f[7]] {
			return
		}
		lat, err1 := strconv.ParseFloat(f[4], 64)
		lon, err2 := strconv.ParseFloat(f[5], 64)
		pop, err3 := strconv.ParseInt(f[14], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || f[1] == "" || f[8] == "" {
			return
		}
		// Four decimals is about eleven metres, far finer than "which town"
		// needs, and it is what keeps the table small.
		places = append(places, strings.Join([]string{
			f[0], clean(f[1]), f[8], clean(f[10]),
			strconv.FormatFloat(lat, 'f', 4, 64), strconv.FormatFloat(lon, 'f', 4, 64),
			// Population is how far a place's name reaches (see geo.reachKm).
			strconv.FormatInt(pop, 10),
		}, "\t"))
		countryUsed[f[8]] = true
		if f[10] != "" {
			admin1Used[f[8]+"."+f[10]] = true
		}
	})
	sortByID(places)

	var regions []string
	eachRow(*admin1, func(f []string) {
		if len(f) >= 2 && admin1Used[f[0]] {
			regions = append(regions, f[0]+"\t"+clean(f[1]))
		}
	})
	sort.Strings(regions)

	var nations []string
	eachRow(*countries, func(f []string) {
		if len(f) >= 5 && countryUsed[f[0]] {
			nations = append(nations, f[0]+"\t"+clean(f[4]))
		}
	})
	sort.Strings(nations)

	write(filepath.Join(*out, "places.tsv.gz"), places)
	write(filepath.Join(*out, "regions.tsv.gz"), regions)
	write(filepath.Join(*out, "countries.tsv.gz"), nations)
	fmt.Printf("%d places, %d regions, %d countries\n", len(places), len(regions), len(nations))
}

func eachRow(path string, fn func([]string)) {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fn(strings.Split(line, "\t"))
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
}

// clean keeps a field from breaking the table it is written into.
func clean(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
}

func sortByID(rows []string) {
	id := func(r string) int {
		n, _ := strconv.Atoi(r[:strings.IndexByte(r, '\t')])
		return n
	}
	sort.Slice(rows, func(i, j int) bool { return id(rows[i]) < id(rows[j]) })
}

func write(path string, rows []string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	// The header is left empty — no name, no modification time — so the
	// output depends on the input alone and a rebuild can be checked against
	// the commit.
	for _, r := range rows {
		if _, err := zw.Write([]byte(r + "\n")); err != nil {
			log.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
