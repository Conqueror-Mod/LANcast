package media

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Audiobooks (ADR 0116). A book is one item whatever it is made of: a folder
// of parts, or a single file. Every book is a `book` container over one or
// more `book_part` files, so a one-file book and a forty-part rip play,
// resume and count the same way.
const (
	KindBook     Kind = "book"
	KindBookPart Kind = "book_part"
)

// LibraryAudiobooks is the library kind that holds books.
const LibraryAudiobooks = "audiobooks"

// IsBookAudio reports whether an audiobooks library indexes the file: every
// music container, and `.m4b`, which only this kind takes. A book among songs
// is the problem ADR 0116 exists to avoid, so a music library still skips it.
func IsBookAudio(path string) bool {
	return IsAudio(path) || strings.EqualFold(filepath.Ext(path), ".m4b")
}

// BookTags is what a part's embedded tags say, as far as books use them.
type BookTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Track       int
	Year        int
	// Composer is where rippers put the narrator; Narrator is a tag of that
	// name, which wins where present (ADR 0116 step 3).
	Composer string
	Narrator string
}

// BookFile is one audio file in an audiobooks library.
type BookFile struct {
	Path string
	Tags BookTags
}

// Book is what a set of files turned out to be.
type Book struct {
	// Key is the container row's identity: the book's folder, or a synthetic
	// path for a single file sitting among other books.
	Key string
	// Dir is the folder a cover is looked for in, or "" for a loose file,
	// whose folder belongs to other books too.
	Dir     string
	Title   string
	Author  string
	Series  string
	Number  int
	Edition string
	Year    int
	// Narrators in billing order: everyone a full-cast drama credits.
	Narrators []string
	Parts     []BookPart
}

// BookPart is one file of a book, in the order it plays.
type BookPart struct {
	Path   string
	Number int
	Title  string
}

var (
	// "(Abridged Novelization)", "- Abridged", "[Abridged]". Not "Unabridged":
	// the word boundary keeps the n out, and an unabridged book is the
	// ordinary case, which needs no mark.
	// A bracketed mark goes with its brackets; a bare one is only the word.
	reAbridged   = regexp.MustCompile(`(?i)[\s._-]*[\(\[][^\)\]]*\babridged\b[^\)\]]*[\)\]]|[\s._-]*\babridged\b`)
	reUnabridged = regexp.MustCompile(`(?i)[\s._-]*[\(\[][^\)\]]*\bunabridged\b[^\)\]]*[\)\]]|[\s._-]*\bunabridged\b`)
	// A leading "03 - ", "03. ", "03) " or "03 ": the book's place in its
	// series folder.
	reLeadingNumber = regexp.MustCompile(`^\s*(\d{1,3})(?:\s*[-.)]\s*|\s+)(.*)$`)
	// "Book 7", "Bk7", "_Bk7_". Bounded by non-letters by hand, because `\b`
	// treats an underscore as part of a word and "Gateways_Bk7_" is a real
	// name.
	reBookNumber = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:book|bk)[\s._]*(\d{1,3})(?:[^0-9]|$)`)
	// The marks that say "this file is part N of a book": "1of4", "(1 of 4)",
	// "disc 2", "Part 03", "CD1", a leading or trailing number. Removed to find
	// what the file says the book is called.
	rePartMarks  = regexp.MustCompile(`(?i)(?:\(\s*)?\b\d{1,3}\s*of\s*\d{1,3}\b(?:\s*\))?|\b(?:disc|disk|cd|part|pt|track)[\s._]*\d{1,3}\b`)
	reEdgeNumber = regexp.MustCompile(`^\s*\d{1,3}\b|\b\d{1,3}\s*$`)
	reSeparators = regexp.MustCompile(`[\s_]+`)
)

// GroupBooks decides which files under one library location make which books.
//
// **A folder holding audio and no folders of audio is one book**, its files
// the parts. That is the convention every audiobook player shares, and the
// alternative — splitting a folder when its files' titles disagree — would
// tear apart the ordinary rip whose files are named after chapters. Files
// loose at the root, or loose beside folders of books, are each a book of
// their own.
//
// Measured against the test library before it was written (ADR 0116): the
// album tag is absent on most parts and wrong where present ("performed
// live", "Star Trek"), so a book's title, series, number and abridgement come
// from its folder, and the tags give the author, the year and part order.
func GroupBooks(root string, files []BookFile) []Book {
	root = filepath.Clean(root)
	byDir := map[string][]BookFile{}
	for _, f := range files {
		d := filepath.Dir(f.Path)
		byDir[d] = append(byDir[d], f)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var books []Book
	for _, d := range dirs {
		group := byDir[d]
		if d == root || holdsBookFolders(d, dirs) {
			for _, f := range group {
				books = append(books, looseBook(root, f))
			}
			continue
		}
		books = append(books, folderBook(root, d, group))
	}
	return books
}

// holdsBookFolders reports whether another folder of audio sits under dir.
func holdsBookFolders(dir string, dirs []string) bool {
	prefix := dir + string(filepath.Separator)
	for _, o := range dirs {
		if strings.HasPrefix(o, prefix) {
			return true
		}
	}
	return false
}

func folderBook(root, dir string, files []BookFile) Book {
	b := Book{Key: dir, Dir: dir}
	name := filepath.Base(dir)

	name, b.Edition = bookEdition(name)
	if n, rest, ok := leadingNumber(name); ok {
		b.Number, name = n, rest
	}

	rel, err := filepath.Rel(root, filepath.Dir(dir))
	var folderAuthor string
	if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		parts := strings.Split(rel, string(filepath.Separator))
		b.Series = tidy(parts[len(parts)-1])
		// Author/Series/Book: the folder above the series names the author,
		// and is used only when no tag does.
		if len(parts) >= 2 {
			folderAuthor = tidy(parts[len(parts)-2])
		}
	}

	parts := orderParts(files)
	if b.Number == 0 {
		b.Number = bookNumber(name)
		if b.Number == 0 && len(parts) > 0 {
			b.Number = bookNumber(stem(parts[0].Path))
		}
	}
	b.Title = stripSeriesPrefix(tidy(name), b.Series)
	if b.Title == "" {
		b.Title = tidy(filepath.Base(dir))
	}
	b.Author = bookAuthor(files, folderAuthor)
	b.Narrators = bookNarrators(files)
	b.Year = bookYear(files)
	b.Parts = partList(parts, b.Title)
	return b
}

func looseBook(root string, f BookFile) Book {
	b := Book{Key: "lancast:book:" + f.Path}
	name, edition := bookEdition(stem(f.Path))
	b.Edition = edition
	if n, rest, ok := leadingNumber(name); ok {
		b.Number, name = n, rest
	}
	if d := filepath.Dir(f.Path); d != root {
		b.Series = tidy(filepath.Base(d))
	}
	b.Title = tidy(f.Tags.Title)
	if b.Title == "" {
		b.Title = stripSeriesPrefix(tidy(name), b.Series)
	}
	if b.Title == "" {
		b.Title = tidy(stem(f.Path))
	}
	b.Author = bookAuthor([]BookFile{f}, "")
	b.Narrators = bookNarrators([]BookFile{f})
	b.Year = f.Tags.Year
	b.Parts = []BookPart{{Path: f.Path, Number: 1, Title: b.Title}}
	return b
}

// bookEdition takes an abridgement mark off a name and reports it. Abridged
// and unabridged recordings are two books (ADR 0116), and "Abridged" is shown
// the way a film's edition is.
func bookEdition(name string) (string, string) {
	if reAbridged.MatchString(name) {
		return strings.TrimSpace(reAbridged.ReplaceAllString(name, "")), "Abridged"
	}
	return strings.TrimSpace(reUnabridged.ReplaceAllString(name, "")), ""
}

func leadingNumber(name string) (int, string, bool) {
	m := reLeadingNumber.FindStringSubmatch(name)
	if m == nil || strings.TrimSpace(m[2]) == "" {
		return 0, name, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, m[2], true
}

func bookNumber(s string) int {
	m := reBookNumber.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// stripSeriesPrefix turns "Gateways - What Lay Beyond" in the Gateways folder
// into "What Lay Beyond". Only behind a separator, so a book whose title
// merely begins with its series' name keeps it.
func stripSeriesPrefix(title, series string) string {
	if series == "" {
		return title
	}
	for _, sep := range []string{" - ", " – ", ": "} {
		if i := strings.Index(title, sep); i > 0 && SortTitle(title[:i]) == SortTitle(series) {
			if rest := strings.TrimSpace(title[i+len(sep):]); rest != "" {
				return rest
			}
		}
	}
	return title
}

// tidy is a folder or tag name made presentable: underscores to spaces,
// runs of space collapsed, separators trimmed from the ends. Not `clean`,
// which reads dots and dashes as a release name's separators — "Spock vs. Q"
// and "Transformations - A Captain Sulu Adventure" were written by a person.
func tidy(s string) string {
	s = reSeparators.ReplaceAllString(s, " ")
	return strings.Trim(s, " -–—,.")
}

func stem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// bookAuthor is the album artist the parts agree on most, else the artist,
// else the folder above the series. "Peel, John" reads "John Peel".
func bookAuthor(files []BookFile, folderAuthor string) string {
	if a := mostCommon(files, func(t BookTags) string { return t.AlbumArtist }); a != "" {
		return personName(a)
	}
	if a := mostCommon(files, func(t BookTags) string { return t.Artist }); a != "" {
		return personName(a)
	}
	return folderAuthor
}

/*
 * bookNarrators is everyone the parts name as reading the book (ADR 0116
 * step 3).
 *
 * A `narrator` tag wins over the composer tag wherever any part carries one:
 * a tagger who wrote the field meant it, and the composer may then hold
 * something else. Without one, the composer tag is where rippers and Audible
 * exports put the narrator.
 *
 * Every name is kept, because a full-cast drama is read by its whole cast and
 * the narrator filter should find it under each of them (answer 6). Ordered by
 * how many parts name somebody, then by who was named first, so a book whose
 * last part adds a guest still leads with its reader.
 */
func bookNarrators(files []BookFile) []string {
	field := func(t BookTags) string { return t.Composer }
	for _, f := range files {
		if strings.TrimSpace(f.Tags.Narrator) != "" {
			field = func(t BookTags) string { return t.Narrator }
			break
		}
	}
	count := map[string]int{}
	var order []string
	for _, f := range files {
		seen := map[string]bool{}
		for _, n := range splitPeople(field(f.Tags)) {
			key := strings.ToLower(n)
			if seen[key] {
				continue
			}
			seen[key] = true
			if count[key] == 0 {
				order = append(order, n)
			}
			count[key]++
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return count[strings.ToLower(order[i])] > count[strings.ToLower(order[j])]
	})
	return order
}

var (
	// "Read by", "Narrated by", "Performed by" in front of the names.
	reReadBy = regexp.MustCompile(`(?i)^\s*(?:read|narrated|performed)\s+by\s+`)
	// The separators a list of people uses: ";" and "/" always, and "&" or
	// "and" between words.
	rePeopleSep = regexp.MustCompile(`\s*[;/]\s*|\s+(?:&|and)\s+`)
)

/*
 * splitPeople turns one tag into the people it names.
 *
 * A comma is the hard case, because it means two things: "Nimoy, Leonard" is
 * one man written surname first, and "Leonard Nimoy, John de Lancie" is two.
 * One comma between two single words is the first; between two names of
 * several words, or with more than one comma, it separates people. personName
 * then turns a surname-first name round.
 */
func splitPeople(s string) []string {
	s = reReadBy.ReplaceAllString(strings.TrimSpace(s), "")
	if s == "" {
		return nil
	}
	var out []string
	for _, piece := range rePeopleSep.Split(s, -1) {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		names := []string{piece}
		if c := strings.Count(piece, ","); c > 1 {
			names = strings.Split(piece, ",")
		} else if c == 1 {
			i := strings.Index(piece, ",")
			left, right := strings.TrimSpace(piece[:i]), strings.TrimSpace(piece[i+1:])
			if strings.Contains(left, " ") && strings.Contains(right, " ") {
				names = []string{left, right}
			}
		}
		for _, n := range names {
			if n = personName(strings.TrimSpace(n)); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

func mostCommon(files []BookFile, field func(BookTags) string) string {
	counts := map[string]int{}
	best, bestN := "", 0
	for _, f := range files {
		v := strings.TrimSpace(field(f.Tags))
		if v == "" {
			continue
		}
		counts[v]++
		if counts[v] > bestN || (counts[v] == bestN && v < best) {
			best, bestN = v, counts[v]
		}
	}
	return best
}

// personName turns "Last, First" round. Only one comma and no "&" or "and",
// so "Earth, Wind & Fire" and "Crosby, Stills and Nash" are left as written.
func personName(s string) string {
	if strings.Count(s, ",") != 1 || strings.Contains(s, "&") ||
		strings.Contains(strings.ToLower(s), " and ") {
		return s
	}
	i := strings.Index(s, ",")
	last, first := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	if last == "" || first == "" {
		return s
	}
	return first + " " + last
}

func bookYear(files []BookFile) int {
	y := 0
	for _, f := range files {
		if f.Tags.Year > 0 && (y == 0 || f.Tags.Year < y) {
			y = f.Tags.Year
		}
	}
	return y
}

// orderParts puts a book's files in playing order: by track number when every
// part has a different one, else by filename read as a person would, so
// "2of4" comes before "10of12" and "disc 2" before "disc 10".
func orderParts(files []BookFile) []BookFile {
	out := append([]BookFile(nil), files...)
	seen := map[int]bool{}
	byTrack := true
	for _, f := range out {
		if f.Tags.Track <= 0 || seen[f.Tags.Track] {
			byTrack = false
			break
		}
		seen[f.Tags.Track] = true
	}
	sort.SliceStable(out, func(i, j int) bool {
		if byTrack {
			return out[i].Tags.Track < out[j].Tags.Track
		}
		return NaturalLess(filepath.Base(out[i].Path), filepath.Base(out[j].Path))
	})
	return out
}

// partList numbers the parts and names them. A part keeps its own title only
// when the parts' titles say different things once their part marks are
// gone — "Spock vs. Q" and "Spock vs. Q: The Sequel" — and is "Part N" when
// they all say the book's name, as "Emissary 1of4" … "Emissary 4of4" do.
func partList(files []BookFile, bookTitle string) []BookPart {
	parts := make([]BookPart, len(files))
	if len(files) == 1 {
		parts[0] = BookPart{Path: files[0].Path, Number: 1, Title: bookTitle}
		return parts
	}
	distinct := true
	seen := map[string]bool{}
	for _, f := range files {
		k := SortTitle(workTitle(partName(f)))
		if k == "" || seen[k] {
			distinct = false
			break
		}
		seen[k] = true
	}
	for i, f := range files {
		title := "Part " + strconv.Itoa(i+1)
		if distinct {
			title = tidy(partName(f))
		}
		parts[i] = BookPart{Path: f.Path, Number: i + 1, Title: title}
	}
	return parts
}

func partName(f BookFile) string {
	if t := strings.TrimSpace(f.Tags.Title); t != "" {
		return t
	}
	return stem(f.Path)
}

// workTitle is what a part says its book is called, with the part marks off.
func workTitle(s string) string {
	s = rePartMarks.ReplaceAllString(s, " ")
	s = reEdgeNumber.ReplaceAllString(s, " ")
	return tidy(s)
}

// NaturalLess orders strings with their runs of digits compared as numbers.
func NaturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ra, rb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ra) && unicode.IsDigit(rb) {
			na, restA := digitRun(a)
			nb, restB := digitRun(b)
			if na != nb {
				return na < nb
			}
			a, b = restA, restB
			continue
		}
		if ra != rb {
			return ra < rb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digitRun(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}
