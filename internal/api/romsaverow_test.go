package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"lancast/internal/store"
)

// hangUp is a request body that cancels the request when it has been read to
// the end: a player that sends its save on exit and closes the connection.
type hangUp struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (h hangUp) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if err == io.EOF {
		h.cancel()
	}
	return n, err
}

/*
 * A save sent on exit keeps its row (R03-5).
 *
 * The row was written on the request's context after the files had rotated,
 * so a player that hung up once the body was sent left the save on disk and
 * no row: the slot answered 404 and the game started without its save.
 */
func TestASaveSentOnExitIsRecorded(t *testing.T) {
	f := newROMFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := hangUp{r: bytes.NewReader([]byte("battery save")), cancel: cancel}
	req := httptest.NewRequest("PUT", "/api/items/"+strconv.FormatInt(f.cart, 10)+"/saves/sram", body).WithContext(ctx)
	req.SetPathValue("id", strconv.FormatInt(f.cart, 10))
	req.SetPathValue("slot", "sram")
	rec := httptest.NewRecorder()
	f.h.srvAPI.putSave(rec, req)
	if ctx.Err() == nil {
		t.Fatal("the body was not read to the end, so this proved nothing")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got, err := f.h.st.GetROMSave(context.Background(), store.LocalUserID, f.cart, "sram")
	if err != nil {
		t.Fatalf("the save is on disk with no row: %v", err)
	}
	if got.SizeBytes != int64(len("battery save")) {
		t.Errorf("row = %+v", got)
	}
}
