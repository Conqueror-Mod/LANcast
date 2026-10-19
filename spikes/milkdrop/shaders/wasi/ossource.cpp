// SPIKE (ADR 0091 step g). Single-threaded "TLS" for hlsl2glslfork on WASI.
#include "osinclude.h"

static void *slots[8];
static bool used[8];

OS_TLSIndex OS_AllocTLSIndex()
{
	for (unsigned i = 0; i < 8; i++) {
		if (!used[i]) {
			used[i] = true;
			slots[i] = 0;
			return i;
		}
	}
	return OS_INVALID_TLS_INDEX;
}

bool OS_SetTLSValue(OS_TLSIndex n, void *v)
{
	if (n >= 8 || !used[n])
		return false;
	slots[n] = v;
	return true;
}

void *OS_GetTLSValue(OS_TLSIndex n)
{
	return (n < 8 && used[n]) ? slots[n] : 0;
}

bool OS_FreeTLSIndex(OS_TLSIndex n)
{
	if (n >= 8 || !used[n])
		return false;
	used[n] = false;
	slots[n] = 0;
	return true;
}
