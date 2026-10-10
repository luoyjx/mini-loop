//go:build darwin && cgo

#include "native_events_darwin.h"
#include <stdlib.h>
#include <string.h>
struct MiniLoopEvents {
 FSEventStreamRef stream;
 CFRunLoopRef loop;
 CFRunLoopSourceRef stop_source;
};
static void mini_loop_stop_on_owner(void *info) {
 MiniLoopEvents *events=info;
 CFRunLoopStop(events->loop);
}
extern void goMiniLoopNativeEvent(uintptr_t key, char *path, uint32_t flags);
static void mini_loop_callback(ConstFSEventStreamRef stream, void *info,
 size_t count, void *paths, const FSEventStreamEventFlags flags[],
 const FSEventStreamEventId ids[]) {
 char **names = paths;
 for (size_t i=0;i<count;i++) goMiniLoopNativeEvent((uintptr_t)info,names[i],flags[i]);
}
MiniLoopEvents *mini_loop_events_start(uintptr_t key, const char *roots) {
 CFMutableArrayRef paths=CFArrayCreateMutable(NULL,0,&kCFTypeArrayCallBacks);
 if (!paths) return NULL;
 const char *cursor=roots;
 while (*cursor) {
  CFStringRef path=CFStringCreateWithCString(NULL,cursor,kCFStringEncodingUTF8);
  if (!path) { CFRelease(paths); return NULL; }
  CFArrayAppendValue(paths,path); CFRelease(path);
  cursor += strlen(cursor)+1;
 }
 FSEventStreamContext context={0,(void*)key,NULL,NULL,NULL};
 FSEventStreamRef stream=FSEventStreamCreate(NULL,mini_loop_callback,&context,
  paths,kFSEventStreamEventIdSinceNow,0.0,
  kFSEventStreamCreateFlagFileEvents|kFSEventStreamCreateFlagNoDefer);
 CFRelease(paths);
 if (!stream) return NULL;
 MiniLoopEvents *events=calloc(1,sizeof(*events));
 if (!events) { FSEventStreamRelease(stream); return NULL; }
 events->stream=stream; events->loop=CFRunLoopGetCurrent();
 CFRunLoopSourceContext stop_context={0};
 stop_context.info=events;
 stop_context.perform=mini_loop_stop_on_owner;
 events->stop_source=CFRunLoopSourceCreate(NULL,0,&stop_context);
 if (!events->stop_source) { FSEventStreamRelease(stream); free(events); return NULL; }
 CFRunLoopAddSource(events->loop,events->stop_source,kCFRunLoopDefaultMode);
 FSEventStreamScheduleWithRunLoop(stream,events->loop,kCFRunLoopDefaultMode);
 if (!FSEventStreamStart(stream)) {
  mini_loop_events_close(events); return NULL;
 }
 return events;
}
void mini_loop_events_run(MiniLoopEvents *events) {
 CFRunLoopRun();
}
void mini_loop_events_request_stop(MiniLoopEvents *events) {
 // The source retains a signal sent before Run begins. Stop executes only on
 // the owner thread, avoiding a lost stop between readiness and loop entry.
 CFRunLoopSourceSignal(events->stop_source);
 CFRunLoopWakeUp(events->loop);
}
void mini_loop_events_close(MiniLoopEvents *events) {
 FSEventStreamStop(events->stream);
 FSEventStreamInvalidate(events->stream);
 FSEventStreamRelease(events->stream);
 CFRunLoopRemoveSource(events->loop,events->stop_source,kCFRunLoopDefaultMode);
 CFRunLoopSourceInvalidate(events->stop_source);
 CFRelease(events->stop_source);
 free(events);
}
