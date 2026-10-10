//go:build darwin && cgo

#include "native_events_darwin.h"
#include <stdlib.h>
#include <string.h>
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
 FSEventStreamScheduleWithRunLoop(stream,events->loop,kCFRunLoopDefaultMode);
 if (!FSEventStreamStart(stream)) {
  FSEventStreamInvalidate(stream); FSEventStreamRelease(stream); free(events); return NULL;
 }
 return events;
}
void mini_loop_events_step(MiniLoopEvents *events) {
 CFRunLoopRunInMode(kCFRunLoopDefaultMode,0.05,true);
}
void mini_loop_events_close(MiniLoopEvents *events) {
 FSEventStreamStop(events->stream);
 FSEventStreamInvalidate(events->stream);
 FSEventStreamRelease(events->stream);
 free(events);
}
