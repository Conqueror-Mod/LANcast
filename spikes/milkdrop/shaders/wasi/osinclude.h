// SPIKE (ADR 0091 step g). hlsl2glslfork's OS layer for WASI.
//
// hlsl2glslfork keeps its pool allocator and "is this thread initialised"
// flag in thread-local storage, through pthread keys on Linux. A WASI
// module here is single-threaded and lives for one conversion, so a slot
// table is all the "TLS" there is. Replaces OSDependent/Linux, unchanged in
// every other respect.
#ifndef __OSINCLUDE_H
#define __OSINCLUDE_H

#include <assert.h>
#include <errno.h>
#include <strings.h>
#include "Include/InitializeGlobals.h"
#include "Include/PoolAlloc.h"

#define _vsnprintf vsnprintf
#define _stricmp strcasecmp

typedef unsigned int OS_TLSIndex;
#define OS_INVALID_TLS_INDEX 0xFFFFFFFF

OS_TLSIndex OS_AllocTLSIndex();
bool        OS_SetTLSValue(OS_TLSIndex nIndex, void *lpvValue);
bool        OS_FreeTLSIndex(OS_TLSIndex nIndex);
void *      OS_GetTLSValue(OS_TLSIndex nIndex);

#endif // __OSINCLUDE_H
