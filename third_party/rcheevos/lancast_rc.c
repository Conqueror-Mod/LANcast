/*
 * lancast_rc.c — the desktop player's door into rcheevos (ADR 0111).
 *
 * rcheevos' rc_client is the whole emulator side of RetroAchievements: it
 * hashes the game, evaluates achievements against memory each frame, and
 * builds every request. It makes no network call itself. This file is the
 * small layer between it and LANcast's Go player, which loads it with
 * syscall rather than cgo:
 *
 *   - Every function here takes and returns integers, byte pointers and
 *     C strings, so the Go side never lays out an rcheevos struct.
 *   - Memory is read here, from the core's regions, rather than through a Go
 *     callback per read: rcheevos reads memory many times a frame.
 *   - A request rc_client wants sent is handed to Go with the callback that
 *     finishes it; Go sends it to the LANcast server and calls
 *     lrc_respond with the answer.
 *   - Events and lists are flattened into one callback shape.
 *
 * One rc_client per process: the player runs one game at a time. Nothing
 * here is patched rcheevos; it is a caller of its public API only.
 */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "rc_client.h"
#include "rc_consoles.h"
#include "rc_libretro.h"

#define LRC_API __declspec(dllexport)

/* Go's side. */
typedef void (*lrc_http_fn)(const char* url, const char* post_data, const char* content_type,
                            void* callback, void* callback_data);
typedef void (*lrc_event_fn)(uint32_t type, uint32_t id, const char* title, const char* description,
                             const char* badge_url, uint32_t points, const char* extra);
typedef void (*lrc_done_fn)(uint32_t op, int32_t result, const char* error_message);
typedef void (*lrc_log_fn)(const char* message);
typedef void (*lrc_core_memory_fn)(uint32_t id, uint8_t** data, size_t* size);

enum {
  LRC_OP_LOGIN = 1,
  LRC_OP_LOAD_GAME = 2
};

/* Extra event types, past rcheevos' own, for list items. */
enum {
  LRC_ITEM_ACHIEVEMENT = 1000
};

static rc_client_t* g_client;
static lrc_http_fn g_http;
static lrc_event_fn g_event;
static lrc_done_fn g_done;
static lrc_log_fn g_log;
static lrc_core_memory_fn g_core_memory;
static rc_libretro_memory_regions_t g_regions;
static int g_have_regions;

static uint32_t RC_CCONV read_memory(uint32_t address, uint8_t* buffer, uint32_t num_bytes, rc_client_t* client) {
  (void)client;
  if (!g_have_regions)
    return 0;
  return rc_libretro_memory_read(&g_regions, address, buffer, num_bytes);
}

static void RC_CCONV server_call(const rc_api_request_t* request, rc_client_server_callback_t callback,
                                 void* callback_data, rc_client_t* client) {
  (void)client;
  if (g_http)
    g_http(request->url, request->post_data, request->content_type, (void*)callback, callback_data);
}

static const char* achievement_badge(const rc_client_achievement_t* a) {
  if (!a)
    return "";
  if (a->unlocked != RC_CLIENT_ACHIEVEMENT_UNLOCKED_NONE)
    return a->badge_url ? a->badge_url : "";
  return a->badge_locked_url ? a->badge_locked_url : (a->badge_url ? a->badge_url : "");
}

static void RC_CCONV event_handler(const rc_client_event_t* event, rc_client_t* client) {
  (void)client;
  if (!g_event)
    return;
  const rc_client_achievement_t* a = event->achievement;
  switch (event->type) {
    case RC_CLIENT_EVENT_ACHIEVEMENT_TRIGGERED:
    case RC_CLIENT_EVENT_ACHIEVEMENT_CHALLENGE_INDICATOR_SHOW:
    case RC_CLIENT_EVENT_ACHIEVEMENT_CHALLENGE_INDICATOR_HIDE:
    case RC_CLIENT_EVENT_ACHIEVEMENT_PROGRESS_INDICATOR_SHOW:
    case RC_CLIENT_EVENT_ACHIEVEMENT_PROGRESS_INDICATOR_UPDATE:
      g_event(event->type, a ? a->id : 0, a ? a->title : "", a ? a->description : "", achievement_badge(a),
              a ? a->points : 0, a ? a->measured_progress : "");
      break;
    case RC_CLIENT_EVENT_SERVER_ERROR: {
      const rc_client_server_error_t* e = event->server_error;
      g_event(event->type, e ? e->related_id : 0, e && e->api ? e->api : "",
              e && e->error_message ? e->error_message : "", "", 0, "");
      break;
    }
    default:
      /* Game completed, disconnected, reconnected, a progress indicator
         hidden: the type alone says it. Leaderboards are hardcore-only and
         never start in softcore (ADR 0111 §4). */
      g_event(event->type, 0, "", "", "", 0, "");
      break;
  }
}

static void RC_CCONV log_message(const char* message, const rc_client_t* client) {
  (void)client;
  if (g_log)
    g_log(message);
}

static void RC_CCONV done_login(int result, const char* error_message, rc_client_t* client, void* userdata) {
  (void)client;
  (void)userdata;
  if (g_done)
    g_done(LRC_OP_LOGIN, result, error_message ? error_message : "");
}

static void RC_CCONV done_load(int result, const char* error_message, rc_client_t* client, void* userdata) {
  (void)client;
  (void)userdata;
  if (g_done)
    g_done(LRC_OP_LOAD_GAME, result, error_message ? error_message : "");
}

/* lrc_init makes the one rc_client, in softcore. Safe to call again. */
LRC_API int32_t lrc_init(lrc_http_fn http, lrc_event_fn event, lrc_done_fn done, lrc_log_fn log) {
  g_http = http;
  g_event = event;
  g_done = done;
  g_log = log;
  if (g_client)
    return 0;
  g_client = rc_client_create(read_memory, server_call);
  if (!g_client)
    return -1;
  rc_client_set_event_handler(g_client, event_handler);
  rc_client_enable_logging(g_client, RC_CLIENT_LOG_LEVEL_WARN, log_message);
  /* rcheevos defaults to hardcore. LANcast is softcore until approved. */
  rc_client_set_hardcore_enabled(g_client, 0);
  return 0;
}

LRC_API void lrc_shutdown(void) {
  if (g_client) {
    rc_client_destroy(g_client);
    g_client = NULL;
  }
  if (g_have_regions) {
    rc_libretro_memory_destroy(&g_regions);
    g_have_regions = 0;
  }
}

/* Finishes a request lrc_http_fn was handed. status is the HTTP status, or
   a negative RC_API_SERVER_RESPONSE_* code when nothing came back. */
LRC_API void lrc_respond(void* callback, void* callback_data, const char* body, size_t body_length, int32_t status) {
  rc_api_server_response_t response;
  response.body = body;
  response.body_length = body_length;
  response.http_status_code = status;
  ((rc_client_server_callback_t)callback)(&response, callback_data);
}

LRC_API size_t lrc_user_agent_clause(char* buffer, size_t buffer_size) {
  if (!g_client)
    return 0;
  return rc_client_get_user_agent_clause(g_client, buffer, buffer_size);
}

LRC_API void lrc_login_with_token(const char* username, const char* token) {
  rc_client_set_hardcore_enabled(g_client, 0);
  rc_client_begin_login_with_token(g_client, username, token, done_login, NULL);
}

static void RC_CCONV core_memory_info(uint32_t id, rc_libretro_core_memory_info_t* info) {
  uint8_t* data = NULL;
  size_t size = 0;
  if (g_core_memory)
    g_core_memory(id, &data, &size);
  info->data = data;
  info->size = size;
}

/*
 * lrc_set_memory takes the core's memory: the map the core declared through
 * RETRO_ENVIRONMENT_SET_MEMORY_MAPS (or NULL), and a function answering
 * retro_get_memory_data/size. Called after the core loads the game, before
 * lrc_load_game. Returns non-zero when some memory was found.
 */
LRC_API int32_t lrc_set_memory(uint32_t console_id, const struct retro_memory_map* mmap, lrc_core_memory_fn core_memory) {
  if (g_have_regions)
    rc_libretro_memory_destroy(&g_regions);
  g_core_memory = core_memory;
  memset(&g_regions, 0, sizeof(g_regions));
  g_have_regions = rc_libretro_memory_init(&g_regions, mmap, core_memory_info, console_id) ? 1 : 0;
  return g_have_regions;
}

/* lrc_load_game identifies the game from its bytes, or its file when data is
   NULL, and loads its achievements. */
LRC_API void lrc_load_game(uint32_t console_id, const char* path, const uint8_t* data, size_t size) {
  rc_client_set_hardcore_enabled(g_client, 0);
  rc_client_begin_identify_and_load_game(g_client, console_id, path, data, size, done_load, NULL);
}

/*
 * One last frame before unloading, while the core's memory is still there.
 * rcheevos v12.5 raises any event still pending while it unloads, after it
 * has cleared client->game, and rc_client_raise_mastery_event reads
 * client->game->subsets: quitting in the moment between the award that
 * completes a set and the next frame crashed the player. A frame raises the
 * pending events while the game is still loaded. Found by
 * cheevos_windows_test.go; not patched in rcheevos.
 */
LRC_API void lrc_unload_game(void) {
  if (!g_client)
    return;
  if (rc_client_is_game_loaded(g_client))
    rc_client_do_frame(g_client);
  rc_client_unload_game(g_client);
}

LRC_API void lrc_do_frame(void) {
  if (g_client)
    rc_client_do_frame(g_client);
}

LRC_API void lrc_idle(void) {
  if (g_client)
    rc_client_idle(g_client);
}

LRC_API void lrc_reset(void) {
  if (g_client)
    rc_client_reset(g_client);
}

LRC_API size_t lrc_progress_size(void) {
  return g_client ? rc_client_progress_size(g_client) : 0;
}

LRC_API int32_t lrc_progress_save(uint8_t* buffer, size_t size) {
  return g_client ? rc_client_serialize_progress_sized(g_client, buffer, size) : RC_INVALID_STATE;
}

/* A NULL buffer clears progress, as rcheevos defines: a state saved without
   any is loaded as "nothing in progress". */
LRC_API int32_t lrc_progress_load(const uint8_t* buffer, size_t size) {
  return g_client ? rc_client_deserialize_progress_sized(g_client, buffer, size) : RC_INVALID_STATE;
}

/* The loaded game: its RetroAchievements id, title and badge. 0 when none. */
LRC_API uint32_t lrc_game(char* title, size_t title_size, char* badge_url, size_t badge_size) {
  const rc_client_game_t* game = g_client ? rc_client_get_game_info(g_client) : NULL;
  if (title_size)
    title[0] = 0;
  if (badge_size)
    badge_url[0] = 0;
  if (!game || game->id == 0)
    return 0;
  if (title_size && game->title)
    strncpy(title, game->title, title_size - 1), title[title_size - 1] = 0;
  if (badge_size && game->badge_url)
    strncpy(badge_url, game->badge_url, badge_size - 1), badge_url[badge_size - 1] = 0;
  return game->id;
}

/* Counts for the loaded game. */
LRC_API void lrc_summary(uint32_t* total, uint32_t* unlocked, uint32_t* points_total, uint32_t* points_unlocked) {
  rc_client_user_game_summary_t s;
  memset(&s, 0, sizeof(s));
  if (g_client)
    rc_client_get_user_game_summary(g_client, &s);
  *total = s.num_core_achievements;
  *unlocked = s.num_unlocked_achievements;
  *points_total = s.points_core;
  *points_unlocked = s.points_unlocked;
}

/*
 * lrc_list_achievements hands every core achievement of the loaded game to
 * the event callback as an LRC_ITEM_ACHIEVEMENT. extra is its state
 * ("locked", "unlocked" or "unsupported"), "|", and its measured progress
 * ("3/10", or empty). Synchronous: done when it returns.
 */
LRC_API void lrc_list_achievements(void) {
  if (!g_client || !g_event)
    return;
  rc_client_achievement_list_t* list =
      rc_client_create_achievement_list(g_client, RC_CLIENT_ACHIEVEMENT_CATEGORY_CORE,
                                        RC_CLIENT_ACHIEVEMENT_LIST_GROUPING_LOCK_STATE);
  if (!list)
    return;
  char extra[64];
  for (uint32_t b = 0; b < list->num_buckets; b++) {
    const rc_client_achievement_bucket_t* bucket = &list->buckets[b];
    for (uint32_t i = 0; i < bucket->num_achievements; i++) {
      const rc_client_achievement_t* a = bucket->achievements[i];
      const char* state = a->unlocked != RC_CLIENT_ACHIEVEMENT_UNLOCKED_NONE ? "unlocked" : "locked";
      if (bucket->bucket_type == RC_CLIENT_ACHIEVEMENT_BUCKET_UNSUPPORTED)
        state = "unsupported";
      snprintf(extra, sizeof(extra), "%s|%s", state, a->measured_progress);
      g_event(LRC_ITEM_ACHIEVEMENT, a->id, a->title, a->description, achievement_badge(a), a->points, extra);
    }
  }
  rc_client_destroy_achievement_list(list);
}

/* The rich presence line, for the menu. */
LRC_API size_t lrc_rich_presence(char* buffer, size_t buffer_size) {
  if (!g_client || !rc_client_has_rich_presence(g_client))
    return 0;
  return rc_client_get_rich_presence_message(g_client, buffer, buffer_size);
}
