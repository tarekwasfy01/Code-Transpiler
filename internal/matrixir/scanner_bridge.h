#ifndef UCT_SCANNER_BRIDGE_H
#define UCT_SCANNER_BRIDGE_H
#include <stdint.h>
#include <stddef.h>

typedef struct {
  const char *source;
  size_t length;
  size_t position;
  size_t token_start;
  size_t mark_end;
  int32_t lookahead;
  uint16_t result_symbol;
} UCTScannerInput;

int uct_scanner_available(const char *language);
void *uct_scanner_create(const char *language);
void uct_scanner_destroy(const char *language, void *payload);
int uct_scanner_scan(const char *language, void *payload, UCTScannerInput *input,
                     const uint8_t *valid_symbols, size_t valid_count);
size_t uct_scanner_serialize(const char *language, void *payload, char *buffer);
void uct_scanner_deserialize(const char *language, void *payload, const char *buffer, size_t length);

#endif
