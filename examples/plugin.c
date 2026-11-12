#include "plugin.h"

#include <string.h>

#include "abi.h"

static envoy_dynamic_module_type_module_buffer lit(const char* s) {
  envoy_dynamic_module_type_module_buffer b = {(char*)s, strlen(s)};
  return b;
}

int plugin_config_init(void* config, const char* name, size_t* id) {
  if (config == NULL) return -1;
  envoy_dynamic_module_type_metrics_result r =
      envoy_dynamic_module_callback_http_filter_config_define_counter(config, lit(name), NULL, 0, id);
  return r == envoy_dynamic_module_type_metrics_result_Success ? 0 : -1;
}

void plugin_on_response_headers(void* stream, void* config, size_t id) {
  envoy_dynamic_module_callback_http_set_header(
      stream, envoy_dynamic_module_type_http_header_type_ResponseHeader, lit("x-native-plugin"),
      lit("ok"));
  envoy_dynamic_module_callback_http_filter_config_increment_counter(config, id, NULL, 0, 1);
}
