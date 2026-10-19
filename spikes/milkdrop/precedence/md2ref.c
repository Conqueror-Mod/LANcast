/*
 * md2ref — MilkDrop 2's own EEL front end, with the code generator replaced
 * by a tree builder, so a statement's parse can be printed and evaluated.
 *
 * Linked UNCHANGED from MilkDrop 2's ns-eel2 (MilkDrop3 repo, code/ns-eel2):
 *   nseel-caltab.c   bison parse tables + driver (from cal.y)
 *   nseel-yylex.c    lexer driver
 *   nseel-lextab.c   lex tables + token actions
 *   nseel-eval.c     identifier/function lookup, variable table
 *   preprocessCode() nseel-compiler.c lines 1010-1457, extracted by build.sh
 * What this file supplies is exactly what nseel-compiler.c supplies to those
 * files: the function table (names and arity copied from fnTable1, in order)
 * and nseel_createCompiled*, which here build nodes instead of x86 code.
 * The statement split copies NSEEL_code_compile (nseel-compiler.c 1522-1533).
 *
 * Evaluation follows the asm in asm-nseel-x86-msvc.c for the operators the
 * precedence question touches: & and | go through fistp qword under
 * _RC_CHOP (GLUE_CALL_CODE sets it, nseel-compiler.c 233-256), so both
 * operands truncate to int64.  _mod is fabs, fistp dword, unsigned div.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <ctype.h>
#include "ns-eel-int.h"

char *md2_preprocessCode(compileContext *ctx, char *expression);

/* ---- function table: names and nParams of fnTable1, same order ---------- */
static functionType tab[] = {
  {"_if",0,0,3},{"_and",0,0,2},{"_or",0,0,2},{"loop",0,0,2},{"while",0,0,1},
  {"_not",0,0,1},{"_equal",0,0,2},{"_noteq",0,0,2},{"_below",0,0,2},
  {"_above",0,0,2},{"_beleq",0,0,2},{"_aboeq",0,0,2},
  {"_set",0,0,2},{"_mod",0,0,2},{"_mulop",0,0,2},{"_divop",0,0,2},
  {"_orop",0,0,2},{"_andop",0,0,2},{"_addop",0,0,2},{"_subop",0,0,2},
  {"_modop",0,0,2},
  {"sin",0,0,1},{"cos",0,0,1},{"tan",0,0,1},{"asin",0,0,1},{"acos",0,0,1},
  {"atan",0,0,1},{"atan2",0,0,2},{"sqr",0,0,1},{"sqrt",0,0,1},{"pow",0,0,2},
  {"_powop",0,0,2},{"exp",0,0,1},{"log",0,0,1},{"log10",0,0,1},{"abs",0,0,1},
  {"min",0,0,2},{"max",0,0,2},{"sign",0,0,1},{"rand",0,0,1},{"floor",0,0,1},
  {"ceil",0,0,1},{"invsqrt",0,0,1},{"sigmoid",0,0,2},{"band",0,0,2},
  {"bor",0,0,2},{"exec2",0,0,2},{"exec3",0,0,3},{"_mem",0,0,1},
  {"_gmem",0,0,1},{"freembuf",0,0,1},{"memcpy",0,0,3},{"memset",0,0,3},
};
functionType *nseel_getFunctionFromTable(int idx)
{
  if (idx < 0 || idx >= (int)(sizeof(tab)/sizeof(tab[0]))) return 0;
  return tab + idx;
}

/* ---- nodes ------------------------------------------------------------ */
enum { N_CONST, N_VAR, N_SIMPLE, N_FN };
typedef struct node { int kind; EEL_F v; EEL_F *addr; int fn; int n; struct node *a[3]; } node;
static compileContext *g_ctx;

static node *mk(int kind) { node *n = calloc(1, sizeof(node)); n->kind = kind; return n; }
INT_PTR nseel_createCompiledValue(compileContext *ctx, EEL_F value, EEL_F *addr)
{
  node *n = mk(addr ? N_VAR : N_CONST); n->v = value; n->addr = addr; return (INT_PTR)n;
}
static INT_PTR fnode(int fntype, INT_PTR fn, int k, INT_PTR c1, INT_PTR c2, INT_PTR c3)
{
  node *n = mk(fntype == MATH_SIMPLE ? N_SIMPLE : N_FN);
  n->fn = (int)fn; n->n = k; n->a[0] = (node *)c1; n->a[1] = (node *)c2; n->a[2] = (node *)c3;
  return (INT_PTR)n;
}
INT_PTR nseel_createCompiledFunction1(compileContext *ctx, int t, INT_PTR fn, INT_PTR c) { return fnode(t, fn, 1, c, 0, 0); }
INT_PTR nseel_createCompiledFunction2(compileContext *ctx, int t, INT_PTR fn, INT_PTR c1, INT_PTR c2) { return fnode(t, fn, 2, c1, c2, 0); }
INT_PTR nseel_createCompiledFunction3(compileContext *ctx, int t, INT_PTR fn, INT_PTR c1, INT_PTR c2, INT_PTR c3) { return fnode(t, fn, 3, c1, c2, c3); }

/* ---- printing: one canonical S-expression grammar shared with eelcanon.mjs */
static const char *varname(EEL_F *p)
{
  static char buf[32];
  int wb, ti;
  if (p >= nseel_globalregs && p < nseel_globalregs + 100) { sprintf(buf, "reg%02d", (int)(p - nseel_globalregs)); return buf; }
  for (wb = 0; wb < g_ctx->varTable_numBlocks; wb++)
    for (ti = 0; ti < NSEEL_VARS_PER_BLOCK; ti++)
      if (g_ctx->varTable_Values[wb] + ti == p) {
        int i; char *s = g_ctx->varTable_Names[wb] + ti * NSEEL_MAX_VARIABLE_NAMELEN;
        for (i = 0; i < NSEEL_MAX_VARIABLE_NAMELEN && s[i]; i++) buf[i] = tolower(s[i]);
        buf[i] = 0; return buf;
      }
  return "?";
}
static const char *simplename[] = { "=", "*", "/", "seq", "+", "-", "&", "|", "neg", "pos" };
static void pr(node *n)
{
  int i;
  switch (n->kind) {
  case N_CONST: printf("%.17g", n->v); return;
  case N_VAR: printf("%s", varname(n->addr)); return;
  case N_SIMPLE:
    printf("(%s", simplename[n->fn]);
    for (i = 0; i < n->n; i++) { printf(" "); pr(n->a[i]); }
    printf(")"); return;
  case N_FN:
    printf("(%s", tab[n->fn].name);
    for (i = 0; i < n->n; i++) { printf(" "); pr(n->a[i]); }
    printf(")"); return;
  }
}

/* ---- evaluation ------------------------------------------------------- */
#define CLOSE 0.00001
static EEL_F ev(node *n);
static EEL_F *lv(node *n) { return n->kind == N_VAR ? n->addr : NULL; }
static long long tr(EEL_F x) { return (long long)x; } /* fistp qword under _RC_CHOP */
static EEL_F ev(node *n)
{
  EEL_F a, b; EEL_F *p;
  switch (n->kind) {
  case N_CONST: return n->v;
  case N_VAR: return *n->addr;
  case N_SIMPLE:
    switch (n->fn) {
    case FN_ASSIGN: a = ev(n->a[1]); if ((p = lv(n->a[0]))) *p = a; return a;
    case FN_MULTIPLY: a = ev(n->a[0]); return a * ev(n->a[1]);
    case FN_DIVIDE: a = ev(n->a[0]); return a / ev(n->a[1]);
    case FN_MODULO: ev(n->a[0]); return ev(n->a[1]); /* exec2: what ';' in parens becomes */
    case FN_ADD: a = ev(n->a[0]); return a + ev(n->a[1]);
    case FN_SUB: a = ev(n->a[0]); return a - ev(n->a[1]);
    case FN_AND: a = ev(n->a[0]); b = ev(n->a[1]); return (EEL_F)(tr(a) & tr(b));
    case FN_OR: a = ev(n->a[0]); b = ev(n->a[1]); return (EEL_F)(tr(a) | tr(b));
    case FN_UMINUS: return -ev(n->a[0]);
    case FN_UPLUS: return ev(n->a[0]);
    }
    break;
  case N_FN: {
    const char *f = tab[n->fn].name;
    if (!strcmp(f, "_if")) return fabs(ev(n->a[0])) > CLOSE ? ev(n->a[1]) : ev(n->a[2]);
    if (!strcmp(f, "_and")) return fabs(ev(n->a[0])) > CLOSE && fabs(ev(n->a[1])) > CLOSE ? 1 : 0;
    if (!strcmp(f, "_or")) return fabs(ev(n->a[0])) > CLOSE || fabs(ev(n->a[1])) > CLOSE ? 1 : 0;
    if (!strcmp(f, "_not")) return fabs(ev(n->a[0])) > CLOSE ? 0 : 1;
    if (!strcmp(f, "_set")) { a = ev(n->a[1]); if ((p = lv(n->a[0]))) *p = a; return a; }
    if (!strcmp(f, "exec2")) { ev(n->a[0]); return ev(n->a[1]); }
    if (!strcmp(f, "exec3")) { ev(n->a[0]); ev(n->a[1]); return ev(n->a[2]); }
    a = ev(n->a[0]); b = n->n > 1 ? ev(n->a[1]) : 0;
    if (!strcmp(f, "_equal")) return fabs(a - b) < CLOSE ? 1 : 0;
    if (!strcmp(f, "_noteq")) return fabs(a - b) < CLOSE ? 0 : 1;
    if (!strcmp(f, "_below")) return a < b;
    if (!strcmp(f, "_above")) return a > b;
    if (!strcmp(f, "_beleq")) return a <= b;
    if (!strcmp(f, "_aboeq")) return a >= b;
    if (f[0]=='_' && strlen(f)>3 && !strcmp(f+strlen(f)-2,"op") && (p = lv(n->a[0]))) {
      /* _addop _subop _mulop _divop _modop _orop _andop: a op= b */
      if (!strcmp(f,"_addop")) return *p = a + b;
      if (!strcmp(f,"_subop")) return *p = a - b;
      if (!strcmp(f,"_mulop")) return *p = a * b;
      if (!strcmp(f,"_divop")) return *p = a / b;
      if (!strcmp(f,"_orop")) return *p = (EEL_F)(tr(a) | tr(b));
      if (!strcmp(f,"_andop")) return *p = (EEL_F)(tr(a) & tr(b));
      if (!strcmp(f,"_modop")) { unsigned d = (unsigned)tr(fabs(b)), x = (unsigned)tr(fabs(a)); return *p = d ? (EEL_F)(x % d) : 0; }
    }
    if (!strcmp(f, "_mod")) { unsigned d = (unsigned)tr(fabs(b)), x = (unsigned)tr(fabs(a)); return d ? (EEL_F)(x % d) : 0; }
    if (!strcmp(f, "pow") || !strcmp(f, "_powop")) return pow(a, b);
    if (!strcmp(f, "sqr")) return a * a;
    if (!strcmp(f, "sqrt")) return sqrt(fabs(a));
    if (!strcmp(f, "abs")) return fabs(a);
    if (!strcmp(f, "min")) return a < b ? a : b;
    if (!strcmp(f, "max")) return a > b ? a : b;
    if (!strcmp(f, "sin")) return sin(a);
    if (!strcmp(f, "cos")) return cos(a);
    if (!strcmp(f, "floor")) return floor(a);
    if (!strcmp(f, "band")) return fabs(a) > CLOSE && fabs(b) > CLOSE;
    if (!strcmp(f, "bor")) return fabs(a) > CLOSE || fabs(b) > CLOSE;
    fprintf(stderr, "eval: %s not modelled\n", f); return 0;
  }
  }
  return 0;
}

/* ---- driver ----------------------------------------------------------- */
/* Usage: md2ref [-e] [-p]  < code      one compile per stdin line.
 *  each line: preprocess, split on ';' as NSEEL_code_compile does, parse.
 *  prints:  PP <preprocessed>   then per statement  T <tree>  or  ERR
 *  -e also evaluates (fresh variables per line) and prints  V name=value  */
static void onevar(void) {}
int main(int argc, char **argv)
{
  int doeval = 0, dopp = 0, i;
  static char line[1 << 16];
  for (i = 1; i < argc; i++) { if (!strcmp(argv[i], "-e")) doeval = 1; if (!strcmp(argv[i], "-p")) dopp = 1; }
  while (fgets(line, sizeof(line), stdin)) {
    compileContext *ctx = calloc(1, sizeof(compileContext));
    char *pp, *expression;
    node *roots[4096]; int nr = 0, bad = 0;
    size_t l = strlen(line);
    while (l && (line[l - 1] == '\n' || line[l - 1] == '\r')) line[--l] = 0;
    g_ctx = ctx;
    memset(nseel_globalregs, 0, sizeof(nseel_globalregs));
    expression = pp = md2_preprocessCode(ctx, line);
    if (dopp) printf("PP %s\n", pp);
    while (*expression) {
      char *expr; void *r;
      while (*expression == ';' || isspace(*expression)) expression++;
      if (!*expression) break;
      expr = expression;
      while (*expression && *expression != ';') expression++;
      if (*expression) *expression++ = 0;
      r = nseel_compileExpression(ctx, expr);
      if (!r) { printf("ERR %s\n", expr); bad = 1; break; }
      if (nr < 4096) roots[nr++] = r;
    }
    if (!bad) for (i = 0; i < nr; i++) { printf("T "); pr(roots[i]); printf("\n"); }
    if (doeval && !bad) {
      int wb, ti;
      for (i = 0; i < nr; i++) ev(roots[i]);
      for (wb = 0; wb < ctx->varTable_numBlocks; wb++)
        for (ti = 0; ti < NSEEL_VARS_PER_BLOCK; ti++) {
          char *s = ctx->varTable_Names[wb] + ti * NSEEL_MAX_VARIABLE_NAMELEN;
          if (!*s) break;
          printf("V %.*s=%.17g\n", NSEEL_MAX_VARIABLE_NAMELEN, s, ctx->varTable_Values[wb][ti]);
        }
    }
    printf("END\n");
    fflush(stdout);
  }
  return 0;
}
