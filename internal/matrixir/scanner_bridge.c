#include "scanner_bridge.h"
#include "tree_sitter/parser.h"
#include <string.h>
#include <stdarg.h>

static void *uct_malloc(size_t n) { return malloc(n); }
static void *uct_calloc(size_t n, size_t s) { return calloc(n, s); }
static void *uct_realloc(void *p, size_t n) { return realloc(p, n); }
static void uct_free(void *p) { free(p); }
void *(*ts_current_malloc)(size_t) = uct_malloc;
void *(*ts_current_calloc)(size_t, size_t) = uct_calloc;
void *(*ts_current_realloc)(void *, size_t) = uct_realloc;
void (*ts_current_free)(void *) = uct_free;

typedef struct {
  void *(*create)(void);
  void (*destroy)(void *);
  bool (*scan)(void *, TSLexer *, const bool *);
  unsigned (*serialize)(void *, char *);
  void (*deserialize)(void *, const char *, unsigned);
} UCTScannerVTable;
static _Thread_local UCTScannerInput *active_input;

/* Each scanner translation unit exports its generated ABI symbols. */
#define DECLARE_SCANNER(prefix) \
  void *tree_sitter_##prefix##_external_scanner_create(void); \
  void tree_sitter_##prefix##_external_scanner_destroy(void *); \
  bool tree_sitter_##prefix##_external_scanner_scan(void *, TSLexer *, const bool *); \
  unsigned tree_sitter_##prefix##_external_scanner_serialize(void *, char *); \
  void tree_sitter_##prefix##_external_scanner_deserialize(void *, const char *, unsigned)

DECLARE_SCANNER(c_sharp);
DECLARE_SCANNER(cpp);
DECLARE_SCANNER(julia);
DECLARE_SCANNER(kotlin);
DECLARE_SCANNER(nim);
DECLARE_SCANNER(python);
DECLARE_SCANNER(r);
DECLARE_SCANNER(rust);
DECLARE_SCANNER(swift);

static UCTScannerVTable table_for(const char *language) {
  UCTScannerVTable t = {0};
#define SELECT(name, prefix) if (strcmp(language, name) == 0) { t.create=tree_sitter_##prefix##_external_scanner_create; t.destroy=tree_sitter_##prefix##_external_scanner_destroy; t.scan=tree_sitter_##prefix##_external_scanner_scan; t.serialize=tree_sitter_##prefix##_external_scanner_serialize; t.deserialize=tree_sitter_##prefix##_external_scanner_deserialize; return t; }
  SELECT("csharp", c_sharp); SELECT("cpp", cpp); SELECT("julia", julia); SELECT("kotlin", kotlin);
  SELECT("nim", nim); SELECT("python", python); SELECT("r", r); SELECT("rust", rust);
  SELECT("swift", swift);
#undef SELECT
  return t;
}

static void advance_cb(TSLexer *lexer, bool skip) {
  UCTScannerInput *in = active_input;
  (void)skip;
  if (!in || !lexer) return;
  if (in->position < in->length) {
    unsigned char c = (unsigned char)in->source[in->position++];
    if ((c & 0x80) == 0) { /* one byte */ }
    else if ((c & 0xE0) == 0xC0 && in->position < in->length) in->position += 1;
    else if ((c & 0xF0) == 0xE0 && in->position + 1 < in->length) in->position += 2;
    else if ((c & 0xF8) == 0xF0 && in->position + 2 < in->length) in->position += 3;
    if (in->position > in->length) in->position = in->length;
  }
  in->lookahead = in->position < in->length ? (unsigned char)in->source[in->position] : 0;
  // Tree-sitter scanners read the lookahead from TSLexer directly. Keeping
  // only the bridge-side copy leaves the scanner observing the same byte
  // forever and causes an apparent parser timeout on whitespace/comments or
  // annotation probes.
  lexer->lookahead = in->lookahead;
}
static void mark_end_cb(TSLexer *lexer) {
  UCTScannerInput *in = active_input;
  in->mark_end = in->position;
}
static uint32_t column_cb(TSLexer *lexer) { (void)lexer; return 0; }
static bool range_start_cb(const TSLexer *lexer) { (void)lexer; return false; }
static bool eof_cb(const TSLexer *lexer) {
  const UCTScannerInput *in = active_input;
  return in->position >= in->length;
}
static void log_cb(const TSLexer *lexer, const char *fmt, ...) { (void)lexer; (void)fmt; }

int uct_scanner_available(const char *language) { return table_for(language).scan != NULL; }
void *uct_scanner_create(const char *language) { UCTScannerVTable t=table_for(language); return t.create ? t.create() : NULL; }
void uct_scanner_destroy(const char *language, void *payload) { UCTScannerVTable t=table_for(language); if (t.destroy && payload) t.destroy(payload); }
int uct_scanner_scan(const char *language, void *payload, UCTScannerInput *in, const uint8_t *valid, size_t count) {
  UCTScannerVTable t=table_for(language); if (!t.scan || !payload || !in) return 0;
  TSLexer lexer = {0};
  lexer.lookahead = in->lookahead;
  active_input = in;
  lexer.advance=advance_cb; lexer.mark_end=mark_end_cb; lexer.get_column=column_cb;
  lexer.is_at_included_range_start=range_start_cb; lexer.eof=eof_cb; lexer.log=log_cb;
  bool flags[512]; memset(flags, 0, sizeof(flags));
  for (size_t i=0; i<count && i<512; i++) flags[i] = valid[i] != 0;
  bool ok=t.scan(payload, &lexer, flags);
  in->lookahead=lexer.lookahead; in->result_symbol=lexer.result_symbol; 
  if (in->mark_end < in->token_start) in->mark_end=in->position;
  active_input = NULL;
  return ok ? 1 : 0;
}
size_t uct_scanner_serialize(const char *language, void *payload, char *buffer) { UCTScannerVTable t=table_for(language); return t.serialize && payload ? t.serialize(payload, buffer) : 0; }
void uct_scanner_deserialize(const char *language, void *payload, const char *buffer, size_t length) { UCTScannerVTable t=table_for(language); if (t.deserialize && payload) t.deserialize(payload, buffer, (unsigned)length); }
