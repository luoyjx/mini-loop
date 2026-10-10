#include <stdint.h>
#include <CoreServices/CoreServices.h>
typedef struct { FSEventStreamRef stream; CFRunLoopRef loop; } MiniLoopEvents;
MiniLoopEvents *mini_loop_events_start(uintptr_t key, const char *roots);
void mini_loop_events_step(MiniLoopEvents *events);
void mini_loop_events_close(MiniLoopEvents *events);
