#!/bin/sh
# Build md2ref from MilkDrop 2's ns-eel2 sources. $1 = path to code/ns-eel2.
set -e
R="$1"; H="$(cd "$(dirname "$0")" && pwd)"; O="$H/out"; mkdir -p "$O"
{
  echo '#include <stdio.h>'
  echo '#include <stdlib.h>'
  echo '#include <string.h>'
  echo '#include <ctype.h>'
  echo '#include "ns-eel-int.h"'
  echo 'static void onCompileNewLine(compileContext *c, int a, int b) {}'
  echo '#define preprocessCode md2_preprocessCode'
  sed -n 1010,1455p "$R/nseel-compiler.c" | sed 's/^static char \*preprocessCode/char *preprocessCode/'
} > "$O/pp.c"
gcc -std=gnu89 -O0 -w -I"$R" -o "$O/md2ref.exe" "$H/md2ref.c" "$O/pp.c" \
  "$R/nseel-caltab.c" "$R/nseel-yylex.c" "$R/nseel-lextab.c" "$R/nseel-eval.c" -lm
echo built "$O/md2ref.exe"
