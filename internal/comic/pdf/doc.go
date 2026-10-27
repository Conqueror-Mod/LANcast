package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
)

/*
 * Finding objects in a file: the cross-reference table or stream the file
 * ends with, its /Prev chain for files edited in place, and object streams
 * for files that compress their small objects. A file whose cross-reference
 * is wrong — common, because many tools write offsets that are a few bytes
 * off — is recovered by reading every "n g obj" in it, which is what every
 * viewer does and the reason a PDF that "opens fine everywhere" can still
 * have a broken table.
 */

// ErrEncrypted is a password-protected or DRM-wrapped file.
var ErrEncrypted = errors.New("pdf: the file is encrypted")

const (
	// maxRead bounds a single object read; a dictionary is a few hundred bytes,
	// a cross-reference table a few kilobytes per thousand objects.
	maxRead = 16 << 20
	// maxDecoded bounds what decompressing an object or cross-reference
	// stream may produce, so a small file cannot expand into gigabytes.
	maxDecoded = 64 << 20
	// maxRecover is the largest file recovery will read whole.
	maxRecover = 1 << 30
)

type entry struct {
	off   int64 // type 1: where the object starts
	stm   int   // type 2: the object stream holding it
	idx   int   // type 2: its index in that stream
	inStm bool
}

type doc struct {
	r         io.ReaderAt
	size      int64
	xref      map[int]entry
	trailer   Dict
	objstms   map[int]map[int]any
	recovered bool
	resolving map[int]bool
}

func open(r io.ReaderAt, size int64) (*doc, error) {
	d := &doc{r: r, size: size, xref: map[int]entry{}, objstms: map[int]map[int]any{}, resolving: map[int]bool{}}
	if err := d.loadXref(); err != nil || d.trailer["Root"] == nil {
		if err := d.recover(); err != nil {
			return nil, err
		}
	}
	if d.trailer["Encrypt"] != nil {
		return nil, ErrEncrypted
	}
	return d, nil
}

// read returns up to n bytes at off.
func (d *doc) read(off int64, n int) ([]byte, error) {
	if off < 0 || off >= d.size {
		return nil, fmt.Errorf("pdf: offset %d is outside the file", off)
	}
	if rem := d.size - off; int64(n) > rem {
		n = int(rem)
	}
	b := make([]byte, n)
	got, err := d.r.ReadAt(b, off)
	if got == n {
		return b, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}

// parseAt runs fn over a buffer starting at off, growing the buffer while fn
// reports that the object runs past it.
func (d *doc) parseAt(off int64, fn func(l *lexer) error) error {
	for n := 4096; ; n *= 4 {
		b, err := d.read(off, n)
		if err != nil {
			return err
		}
		err = fn(&lexer{b: b})
		if !errors.Is(err, errShort) || len(b) < n || n >= maxRead {
			if errors.Is(err, errShort) {
				return fmt.Errorf("pdf: object at %d does not end", off)
			}
			return err
		}
	}
}

func (d *doc) loadXref() error {
	tailLen := int64(4096)
	if tailLen > d.size {
		tailLen = d.size
	}
	tail, err := d.read(d.size-tailLen, int(tailLen))
	if err != nil {
		return err
	}
	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		return errors.New("pdf: no startxref")
	}
	l := &lexer{b: tail, pos: i + len("startxref")}
	l.skipSpace()
	off, err := strconv.ParseInt(string(l.word()), 10, 64)
	if err != nil {
		return errors.New("pdf: bad startxref")
	}
	seen := map[int64]bool{}
	for off > 0 && !seen[off] && len(seen) < 64 {
		seen[off] = true
		trailer, err := d.xrefSection(off)
		if err != nil {
			return err
		}
		if d.trailer == nil {
			d.trailer = trailer
		}
		// A hybrid file keeps its compressed objects in a stream beside the
		// table; the table lists them as free.
		if x, ok := trailer["XRefStm"].(int64); ok && !seen[x] {
			seen[x] = true
			if _, err := d.xrefSection(x); err != nil {
				return err
			}
		}
		prev, _ := trailer["Prev"].(int64)
		off = prev
	}
	return nil
}

// xrefSection reads one table or stream at off, keeping entries already
// known (a newer section wins), and returns its trailer dictionary.
func (d *doc) xrefSection(off int64) (Dict, error) {
	var trailer Dict
	var isStream bool
	err := d.parseAt(off, func(l *lexer) error {
		if !l.keyword("xref") {
			isStream = true
			return nil
		}
		type pending struct {
			num int
			e   entry
		}
		var got []pending
		for {
			l.skipSpace()
			if l.keyword("trailer") {
				t, err := l.object(0)
				if err != nil {
					return err
				}
				td, ok := t.(Dict)
				if !ok {
					return errors.New("pdf: trailer is not a dictionary")
				}
				trailer = td
				for _, p := range got {
					if _, ok := d.xref[p.num]; !ok {
						d.xref[p.num] = p.e
					}
				}
				return nil
			}
			start, err1 := strconv.Atoi(string(l.word()))
			l.skipSpace()
			count, err2 := strconv.Atoi(string(l.word()))
			if l.atEnd() {
				return errShort
			}
			if err1 != nil || err2 != nil || start < 0 || count < 0 || count > 10_000_000 {
				return errors.New("pdf: bad cross-reference subsection")
			}
			for k := 0; k < count; k++ {
				l.skipSpace()
				o := l.word()
				l.skipSpace()
				l.word()
				l.skipSpace()
				kind := l.word()
				if l.atEnd() {
					return errShort
				}
				if string(kind) == "n" {
					v, err := strconv.ParseInt(string(o), 10, 64)
					if err != nil {
						return errors.New("pdf: bad cross-reference entry")
					}
					// Free entries are not recorded: a hybrid file marks its
					// compressed objects free here and lists them elsewhere.
					if v > 0 {
						got = append(got, pending{start + k, entry{off: v}})
					}
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if !isStream {
		return trailer, nil
	}
	obj, err := d.objectAt(off)
	if err != nil {
		return nil, err
	}
	s, ok := obj.(*Stream)
	if !ok || s.Dict["Type"] != Name("XRef") {
		return nil, errors.New("pdf: startxref points at neither a table nor a stream")
	}
	data, err := d.decode(s, maxDecoded)
	if err != nil {
		return nil, err
	}
	w, _ := s.Dict["W"].(Array)
	if len(w) != 3 {
		return nil, errors.New("pdf: cross-reference stream has no /W")
	}
	var ws [3]int
	row := 0
	for i, v := range w {
		n, ok := v.(int64)
		if !ok || n < 0 || n > 8 {
			return nil, errors.New("pdf: bad /W")
		}
		ws[i] = int(n)
		row += int(n)
	}
	if row == 0 {
		return nil, errors.New("pdf: bad /W")
	}
	index, _ := s.Dict["Index"].(Array)
	if index == nil {
		size, _ := s.Dict["Size"].(int64)
		index = Array{int64(0), size}
	}
	pos := 0
	field := func(width, def int) int {
		if width == 0 {
			return def
		}
		v := 0
		for k := 0; k < width; k++ {
			v = v<<8 | int(data[pos])
			pos++
		}
		return v
	}
	for i := 0; i+1 < len(index); i += 2 {
		start, ok1 := index[i].(int64)
		count, ok2 := index[i+1].(int64)
		if !ok1 || !ok2 || start < 0 || count < 0 {
			return nil, errors.New("pdf: bad /Index")
		}
		for k := int64(0); k < count; k++ {
			if pos+row > len(data) {
				return s.Dict, nil // a short stream: keep what it had
			}
			typ := field(ws[0], 1)
			f2 := field(ws[1], 0)
			f3 := field(ws[2], 0)
			num := int(start + k)
			if _, known := d.xref[num]; known {
				continue
			}
			switch typ {
			case 1:
				if f2 > 0 {
					d.xref[num] = entry{off: int64(f2)}
				}
			case 2:
				d.xref[num] = entry{stm: f2, idx: f3, inStm: true}
			}
		}
	}
	return s.Dict, nil
}

// objectAt parses the indirect object starting at off.
func (d *doc) objectAt(off int64) (any, error) {
	var out any
	err := d.parseAt(off, func(l *lexer) error {
		if _, _, err := l.header(); err != nil {
			return err
		}
		v, err := l.object(0)
		if err != nil {
			return err
		}
		dict, isDict := v.(Dict)
		if isDict {
			ok, err := l.streamStart()
			if err != nil {
				return err
			}
			if ok {
				out = &Stream{Dict: dict, Off: off + int64(l.pos), Len: -1}
				return nil
			}
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s, ok := out.(*Stream); ok {
		if err := d.streamLength(s); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// streamLength settles a stream's length: /Length if what follows it is
// "endstream", otherwise the distance to the next "endstream".
func (d *doc) streamLength(s *Stream) error {
	if n, ok := d.resolve(s.Dict["Length"]).(int64); ok && n >= 0 && s.Off+n <= d.size {
		b, err := d.read(s.Off+n, 32)
		if err == nil {
			l := &lexer{b: b}
			if l.keyword("endstream") {
				s.Len = n
				return nil
			}
		}
	}
	// Search forward for the end.
	const chunk = 1 << 20
	for off := s.Off; off < d.size && off-s.Off < maxRead*4; off += chunk - 16 {
		b, err := d.read(off, chunk)
		if err != nil {
			return err
		}
		if i := bytes.Index(b, []byte("endstream")); i >= 0 {
			end := off + int64(i)
			// The end of line before "endstream" is not data.
			if t, err := d.read(end-2, 2); err == nil {
				switch {
				case t[0] == '\r' && t[1] == '\n':
					end -= 2
				case t[1] == '\n' || t[1] == '\r':
					end--
				}
			}
			if end < s.Off {
				end = s.Off
			}
			s.Len = end - s.Off
			return nil
		}
		if len(b) < chunk {
			break
		}
	}
	return errors.New("pdf: a stream does not end")
}

// get returns object num, resolved from wherever the file keeps it.
func (d *doc) get(num int) (any, error) {
	e, ok := d.xref[num]
	if !ok {
		return nil, nil // a missing object is null
	}
	if !e.inStm {
		v, err := d.objectAt(e.off)
		if err != nil && !d.recovered {
			// The table pointed somewhere wrong: rebuild it from the file
			// and look again.
			if rerr := d.recover(); rerr != nil {
				return nil, err
			}
			return d.get(num)
		}
		return v, err
	}
	objs, ok := d.objstms[e.stm]
	if !ok {
		var err error
		objs, err = d.loadObjStm(e.stm)
		if err != nil {
			return nil, err
		}
		d.objstms[e.stm] = objs
	}
	return objs[num], nil
}

func (d *doc) loadObjStm(num int) (map[int]any, error) {
	if d.resolving[num] {
		return nil, errors.New("pdf: object stream refers to itself")
	}
	d.resolving[num] = true
	defer delete(d.resolving, num)
	v, err := d.get(num)
	if err != nil {
		return nil, err
	}
	s, ok := v.(*Stream)
	if !ok {
		return nil, fmt.Errorf("pdf: object %d is not an object stream", num)
	}
	return d.parseObjStm(s)
}

// parseObjStm reads the objects an object stream holds.
func (d *doc) parseObjStm(s *Stream) (map[int]any, error) {
	data, err := d.decode(s, maxDecoded)
	if err != nil {
		return nil, err
	}
	n, _ := s.Dict["N"].(int64)
	first, _ := s.Dict["First"].(int64)
	if n < 0 || first < 0 || first > int64(len(data)) || n > 1_000_000 {
		return nil, errors.New("pdf: bad object stream")
	}
	l := &lexer{b: data[:first]}
	type at struct{ num, off int }
	var heads []at
	for k := int64(0); k < n; k++ {
		l.skipSpace()
		a, err1 := strconv.Atoi(string(l.word()))
		l.skipSpace()
		b, err2 := strconv.Atoi(string(l.word()))
		if err1 != nil || err2 != nil {
			break
		}
		heads = append(heads, at{a, b})
	}
	out := make(map[int]any, len(heads))
	body := data[first:]
	for _, h := range heads {
		if h.off < 0 || h.off >= len(body) {
			continue
		}
		ol := &lexer{b: body, pos: h.off}
		v, err := ol.object(0)
		if err != nil {
			continue
		}
		out[h.num] = v
	}
	return out, nil
}

// resolve follows references until it reaches a direct object.
func (d *doc) resolve(v any) any {
	for i := 0; i < 32; i++ {
		r, ok := v.(Ref)
		if !ok {
			return v
		}
		if d.resolving[-1-r.Num] {
			return nil
		}
		d.resolving[-1-r.Num] = true
		got, err := d.get(r.Num)
		delete(d.resolving, -1-r.Num)
		if err != nil {
			return nil
		}
		v = got
	}
	return nil
}

func (d *doc) dict(v any) Dict {
	switch t := d.resolve(v).(type) {
	case Dict:
		return t
	case *Stream:
		return t.Dict
	}
	return nil
}

// raw reads a stream's bytes as they are in the file, up to limit.
func (d *doc) raw(s *Stream, limit int64) ([]byte, error) {
	if s.Len > limit {
		return nil, fmt.Errorf("pdf: a stream of %d bytes is larger than %d", s.Len, limit)
	}
	return d.read(s.Off, int(s.Len))
}

// filters lists a stream's filters by name.
func (d *doc) filters(s *Stream) []Name {
	switch f := d.resolve(s.Dict["Filter"]).(type) {
	case Name:
		return []Name{f}
	case Array:
		out := make([]Name, 0, len(f))
		for _, v := range f {
			if n, ok := d.resolve(v).(Name); ok {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

// decode returns a stream's data with its filters undone. Only Flate, the one
// filter object and cross-reference streams use, is supported.
func (d *doc) decode(s *Stream, limit int64) ([]byte, error) {
	data, err := d.raw(s, maxRead*4)
	if err != nil {
		return nil, err
	}
	fs := d.filters(s)
	switch {
	case len(fs) == 0:
		return data, nil
	case len(fs) == 1 && fs[0] == "FlateDecode":
	default:
		return nil, fmt.Errorf("pdf: filter %v is not supported here", fs)
	}
	out, err := inflate(data, limit)
	if err != nil {
		return nil, err
	}
	parms := d.dict(s.Dict["DecodeParms"])
	if a, ok := d.resolve(s.Dict["DecodeParms"]).(Array); ok && len(a) == 1 {
		parms = d.dict(a[0])
	}
	return unpredict(out, parms)
}

// inflate undoes Flate, refusing output past limit. A stream truncated near
// its end — common in files that otherwise read fine — keeps what it had.
func inflate(data []byte, limit int64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("pdf: a stream decompresses past %d bytes", limit)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	return out, nil
}

// unpredict undoes a PNG predictor (10 and up), the one in common use.
func unpredict(data []byte, parms Dict) ([]byte, error) {
	pred, _ := parms["Predictor"].(int64)
	if pred < 10 {
		if pred > 1 {
			return nil, fmt.Errorf("pdf: predictor %d is not supported", pred)
		}
		return data, nil
	}
	colors, bpc, columns := int64(1), int64(8), int64(1)
	if v, ok := parms["Colors"].(int64); ok {
		colors = v
	}
	if v, ok := parms["BitsPerComponent"].(int64); ok {
		bpc = v
	}
	if v, ok := parms["Columns"].(int64); ok {
		columns = v
	}
	if colors < 1 || colors > 4 || bpc < 1 || bpc > 16 || columns < 1 || columns > 1<<20 {
		return nil, errors.New("pdf: bad predictor parameters")
	}
	bpp := int((colors*bpc + 7) / 8)
	rowLen := int((colors*bpc*columns + 7) / 8)
	stride := rowLen + 1
	rows := len(data) / stride
	out := make([]byte, rows*rowLen)
	prev := make([]byte, rowLen)
	for r := 0; r < rows; r++ {
		in := data[r*stride : (r+1)*stride]
		ft, cur := in[0], out[r*rowLen:(r+1)*rowLen]
		copy(cur, in[1:])
		for i := 0; i < rowLen; i++ {
			var a, c byte
			if i >= bpp {
				a = cur[i-bpp]
				c = prev[i-bpp]
			}
			b := prev[i]
			switch ft {
			case 0:
			case 1:
				cur[i] += a
			case 2:
				cur[i] += b
			case 3:
				cur[i] += byte((int(a) + int(b)) / 2)
			case 4:
				cur[i] += paeth(a, b, c)
			default:
				return nil, fmt.Errorf("pdf: PNG filter %d", ft)
			}
		}
		prev = cur
	}
	return out, nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var reObj = regexp.MustCompile(`(?:^|[\s>\]\)])(\d{1,10})\s+(\d{1,5})\s+obj\b`)

// recover rebuilds the cross-reference by reading every object header in the
// file, the last definition of a number winning as an incremental update's
// would, then adds what object streams hold and finds the catalogue.
func (d *doc) recover() error {
	if d.recovered {
		return errors.New("pdf: the file could not be read")
	}
	d.recovered = true
	if d.size > maxRecover {
		return errors.New("pdf: the file's index is broken and it is too large to rebuild")
	}
	all, err := d.read(0, int(d.size))
	if err != nil {
		return err
	}
	d.xref = map[int]entry{}
	d.objstms = map[int]map[int]any{}
	var order []int
	for _, m := range reObj.FindAllSubmatchIndex(all, -1) {
		num, _ := strconv.Atoi(string(all[m[2]:m[3]]))
		if _, seen := d.xref[num]; !seen {
			order = append(order, num)
		}
		d.xref[num] = entry{off: int64(m[2])}
	}
	if len(d.xref) == 0 {
		return errors.New("pdf: not a PDF")
	}
	// What object streams hold, unless also written out directly.
	for _, num := range order {
		v, err := d.objectAt(d.xref[num].off)
		if err != nil {
			continue
		}
		s, ok := v.(*Stream)
		if !ok || s.Dict["Type"] != Name("ObjStm") {
			continue
		}
		// Parsed from the stream in hand, not looked up by number: recovery
		// can start while this very stream is being loaded, and a lookup
		// would be refused as a loop.
		objs, err := d.parseObjStm(s)
		if err != nil {
			continue
		}
		for n := range objs {
			if _, direct := d.xref[n]; !direct {
				d.xref[n] = entry{stm: num, inStm: true}
			}
		}
		d.objstms[num] = objs
	}
	// The trailer: the last "trailer" dictionary, or a cross-reference
	// stream's, or failing both a catalogue found by its type.
	trailer := Dict{}
	if i := bytes.LastIndex(all, []byte("trailer")); i >= 0 {
		l := &lexer{b: all, pos: i + len("trailer")}
		if t, err := l.object(0); err == nil {
			if td, ok := t.(Dict); ok {
				trailer = td
			}
		}
	}
	if trailer["Root"] == nil {
		for _, num := range order {
			v, _ := d.get(num)
			dict := d.dict(v)
			if dict == nil {
				continue
			}
			if dict["Type"] == Name("XRef") && dict["Root"] != nil {
				trailer = dict
			}
			if dict["Type"] == Name("Catalog") && trailer["Root"] == nil {
				trailer["Root"] = Ref{Num: num}
			}
		}
	}
	if trailer["Root"] == nil {
		return errors.New("pdf: no catalogue")
	}
	d.trailer = trailer
	return nil
}
