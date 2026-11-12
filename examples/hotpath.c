#include "hotpath.h"

#include <stdlib.h>

#include "abi.h"

/* Matcher state carried across chunks (Knuth-Morris-Pratt). */
typedef struct {
  const char* pat;
  size_t n;
  size_t* fail;
  size_t k;
} matcher;

static int feed(matcher* m, const char* data, size_t len) {
  for (size_t i = 0; i < len; i++) {
    while (m->k > 0 && data[i] != m->pat[m->k]) m->k = m->fail[m->k - 1];
    if (data[i] == m->pat[m->k]) m->k++;
    if (m->k == m->n) return 1;
  }
  return 0;
}

static int scan_body(void* stream, envoy_dynamic_module_type_http_body_type type, matcher* m) {
  size_t chunks = envoy_dynamic_module_callback_http_get_body_chunks_size(stream, type);
  if (chunks == 0) return 0;
  envoy_dynamic_module_type_envoy_buffer* v = malloc(chunks * sizeof(*v));
  if (v == NULL) return 0;
  int found = 0;
  /* The chunks point into Envoy's own buffer and are valid until the end of
   * the hook. They are read in place and never copied or kept. */
  if (envoy_dynamic_module_callback_http_get_body_chunks(stream, type, v)) {
    for (size_t i = 0; i < chunks && !found; i++) found = feed(m, v[i].ptr, v[i].length);
  }
  free(v);
  return found;
}

int body_contains(void* stream, const char* pattern, size_t n) {
  if (n == 0) return 1;
  size_t* fail = calloc(n, sizeof(size_t));
  if (fail == NULL) return 0;
  for (size_t i = 1, k = 0; i < n; i++) {
    while (k > 0 && pattern[i] != pattern[k]) k = fail[k - 1];
    if (pattern[i] == pattern[k]) k++;
    fail[i] = k;
  }
  matcher m = {pattern, n, fail, 0};
  int found = scan_body(stream, envoy_dynamic_module_type_http_body_type_BufferedRequestBody, &m) ||
              scan_body(stream, envoy_dynamic_module_type_http_body_type_ReceivedRequestBody, &m);
  free(fail);
  return found;
}
