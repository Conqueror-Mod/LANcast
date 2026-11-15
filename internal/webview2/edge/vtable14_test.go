//go:build windows

package edge

import (
	"testing"
	"unsafe"
)

/*
 * The slot ICoreWebView2_14's first method lands on, counted from the start
 * of the vtable: IUnknown's 3, ICoreWebView2's 58, _2's 7, _3's 5, then _4 to
 * _13's 33. Calling through a miscounted table calls some other method with
 * the certificate handler as its argument, and nothing would say so until a
 * certificate error arrived in somebody's window. This test is the count.
 */
func TestTheVtableReachesInterface14(t *testing.T) {
	var v iCoreWebView2_14Vtbl
	slot := unsafe.Offsetof(v.AddServerCertificateErrorDetected) / unsafe.Sizeof(ComProc(0))
	if slot != 3+58+7+5+33 {
		t.Fatalf("AddServerCertificateErrorDetected is slot %d, want %d", slot, 3+58+7+5+33)
	}
	var base iCoreWebView2Vtbl
	if n := unsafe.Sizeof(base) / unsafe.Sizeof(ComProc(0)); n != 3+58 {
		t.Fatalf("ICoreWebView2 has %d slots, want %d; the count above depends on it", n, 3+58)
	}
}
