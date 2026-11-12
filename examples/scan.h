/* scan.h: a small request scanner. It knows nothing about Go. A caller gives
 * it the Envoy stream pointer, and it reads request headers with the dynamic
 * modules ABI callbacks. */
#ifndef SCAN_H
#define SCAN_H

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
  int blocked;     /* 1 when a rule matched, 0 otherwise. */
  int rule_index;  /* Index of the first matching rule, or -1. */
} scan_verdict;

/* A rule matches when the header named `header` (or any header when `header`
 * is NULL) has a value that contains `needle`, ignoring ASCII case. */
typedef struct {
  const char* header;
  const char* needle;
} scan_rule;

/* scan_request_headers checks the request headers of `stream`, the
 * envoy_dynamic_module_type_http_filter_envoy_ptr that Envoy passed to the
 * module. Call it only on the stream's owner thread, during a callback. */
scan_verdict scan_request_headers(void* stream, const scan_rule* rules, int n);

#ifdef __cplusplus
}
#endif
#endif
