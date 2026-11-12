#include "scan.h"

#include <ctype.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

#include "abi.h"

static bool contains_nocase(const char* hay, size_t hay_len, const char* needle) {
  size_t n = strlen(needle);
  if (n == 0) return true;
  if (hay_len < n) return false;
  for (size_t i = 0; i + n <= hay_len; i++) {
    size_t j = 0;
    while (j < n && tolower((unsigned char)hay[i + j]) == tolower((unsigned char)needle[j])) j++;
    if (j == n) return true;
  }
  return false;
}

static bool name_equals_nocase(const char* name, size_t len, const char* want) {
  if (strlen(want) != len) return false;
  for (size_t i = 0; i < len; i++) {
    if (tolower((unsigned char)name[i]) != tolower((unsigned char)want[i])) return false;
  }
  return true;
}

scan_verdict scan_request_headers(void* stream, const scan_rule* rules, int n) {
  scan_verdict v = {0, -1};
  size_t count = envoy_dynamic_module_callback_http_get_headers_size(
      stream, envoy_dynamic_module_type_http_header_type_RequestHeader);
  if (count == 0) return v;
  envoy_dynamic_module_type_envoy_http_header* headers = malloc(count * sizeof(*headers));
  if (headers == NULL) return v;
  if (envoy_dynamic_module_callback_http_get_headers(
          stream, envoy_dynamic_module_type_http_header_type_RequestHeader, headers)) {
    /* The key and value buffers are owned by Envoy and valid until the end of
     * the current hook. They are only read here, never kept. */
    for (size_t h = 0; h < count && !v.blocked; h++) {
      for (int r = 0; r < n; r++) {
        if (rules[r].header != NULL &&
            !name_equals_nocase(headers[h].key_ptr, headers[h].key_length, rules[r].header)) {
          continue;
        }
        if (contains_nocase(headers[h].value_ptr, headers[h].value_length, rules[r].needle)) {
          v.blocked = 1;
          v.rule_index = r;
          break;
        }
      }
    }
  }
  free(headers);
  return v;
}
