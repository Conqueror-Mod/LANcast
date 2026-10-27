package probe

import (
	"reflect"
	"testing"
)

// The shape `ffprobe -show_chapters -print_format json` writes for an MKV.
const mkvChapters = `{"chapters":[
 {"id":1,"time_base":"1/1000000000","start":0,"start_time":"0.000000","end":312000000000,"end_time":"312.000000","tags":{"title":"Opening"}},
 {"id":2,"time_base":"1/1000000000","start":312000000000,"start_time":"312.000000","end":1500500000000,"end_time":"1500.500000","tags":{"title":"The Heist"}},
 {"id":3,"time_base":"1/1000000000","start":1500500000000,"start_time":"1500.500000","end":2000000000000,"end_time":"2000.000000","tags":{"title":"Credits"}}
]}`

func TestChaptersFromAnMKV(t *testing.T) {
	got, err := ParseChapters([]byte(mkvChapters))
	if err != nil {
		t.Fatal(err)
	}
	want := []Chapter{
		{0, 312000, "Opening"},
		{312000, 1500500, "The Heist"},
		{1500500, 2000000, "Credits"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// MP4 chapter titles arrive under a capitalised key from some muxers.
func TestATitleUnderAnyCase(t *testing.T) {
	got, _ := ParseChapters([]byte(`{"chapters":[
	 {"start_time":"0.0","end_time":"10.0","tags":{"TITLE":"One"}},
	 {"start_time":"10.0","end_time":"20.0","tags":{"Title":"Two"}}]}`))
	if len(got) != 2 || got[0].Title != "One" || got[1].Title != "Two" {
		t.Fatalf("got %+v", got)
	}
}

func TestUntitledChaptersAreNumbered(t *testing.T) {
	got, _ := ParseChapters([]byte(`{"chapters":[
	 {"start_time":"0.0","end_time":"10.0"},
	 {"start_time":"10.0","end_time":"20.0","tags":{"title":"   "}}]}`))
	if len(got) != 2 || got[0].Title != "Chapter 1" || got[1].Title != "Chapter 2" {
		t.Fatalf("got %+v", got)
	}
}

// One chapter covering the whole file says nothing, and is not shown.
func TestASingleChapterIsNone(t *testing.T) {
	got, err := ParseChapters([]byte(`{"chapters":[{"start_time":"0.0","end_time":"5400.0","tags":{"title":"Chapter 1"}}]}`))
	if err != nil || len(got) != 0 || got == nil {
		t.Fatalf("got %#v, %v; want an empty, non-nil list", got, err)
	}
}

func TestNoChaptersAtAll(t *testing.T) {
	got, err := ParseChapters([]byte(`{}`))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v", got, err)
	}
}

// Out of order, duplicated and unreadable times: sorted, deduplicated and
// dropped, so "next chapter" always moves forward.
func TestDisorderlyChapters(t *testing.T) {
	got, _ := ParseChapters([]byte(`{"chapters":[
	 {"start_time":"600.0","end_time":"900.0","tags":{"title":"C"}},
	 {"start_time":"0.0","end_time":"300.0","tags":{"title":"A"}},
	 {"start_time":"300.0","end_time":"700.0","tags":{"title":"B"}},
	 {"start_time":"300.0","end_time":"600.0","tags":{"title":"B again"}},
	 {"start_time":"N/A","end_time":"10.0","tags":{"title":"broken"}}]}`))
	want := []Chapter{{0, 300000, "A"}, {300000, 600000, "B"}, {600000, 900000, "C"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestMalformedJSONIsAnError(t *testing.T) {
	if _, err := ParseChapters([]byte(`{"chapters":`)); err == nil {
		t.Fatal("malformed output was accepted")
	}
}
