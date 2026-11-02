/* See lancast_cheevos.h. */
#include "lancast_cheevos.h"

#include <stdlib.h>
#include <string.h>

#include "rc_client.h"
#include "rc_hash.h"
#include "rc_libretro.h"

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
typedef CRITICAL_SECTION lc_lock_t;
#define lc_lock_init(l) InitializeCriticalSection(l)
#define lc_lock(l) EnterCriticalSection(l)
#define lc_unlock(l) LeaveCriticalSection(l)
#else
#include <pthread.h>
typedef pthread_mutex_t lc_lock_t;
#define lc_lock_init(l) pthread_mutex_init(l, NULL)
#define lc_lock(l) pthread_mutex_lock(l)
#define lc_unlock(l) pthread_mutex_unlock(l)
#endif

static rc_client_t* client;
static lc_request_fn on_request;
static lc_event_fn on_event;
static lc_log_fn on_log;
static lc_done_fn on_done;

/*
 * Requests in flight. rcheevos hands each HTTP call a callback to invoke with
 * the answer; Go answers later, from its own goroutine. An answer for a
 * request this table no longer holds (the session ended, the client was
 * destroyed) is dropped instead of calling into freed memory.
 */
#define LC_MAX_PENDING 64
typedef struct {
	uint32_t id;
	rc_client_server_callback_t callback;
	void* data;
} lc_pending_t;
static lc_pending_t pending[LC_MAX_PENDING];
static uint32_t next_id = 1;
static lc_lock_t pending_lock;
static int lock_ready;

static void ensure_lock(void) {
	if (!lock_ready) {
		lc_lock_init(&pending_lock);
		lock_ready = 1;
	}
}

/* The game's memory, registered once per game. */
static rc_libretro_memory_regions_t regions;
static rc_libretro_core_memory_info_t core_ram[2]; /* system, save */

static void RC_CCONV core_memory_info(uint32_t id, rc_libretro_core_memory_info_t* info) {
	info->data = NULL;
	info->size = 0;
	if (id == RETRO_MEMORY_SYSTEM_RAM) *info = core_ram[0];
	else if (id == RETRO_MEMORY_SAVE_RAM) *info = core_ram[1];
}

static uint32_t RC_CCONV read_memory(uint32_t address, uint8_t* buffer, uint32_t num_bytes, rc_client_t* c) {
	(void)c;
	return rc_libretro_memory_read(&regions, address, buffer, num_bytes);
}

static void RC_CCONV server_call(const rc_api_request_t* request, rc_client_server_callback_t callback,
	void* callback_data, rc_client_t* c) {
	(void)c;
	uint32_t id = 0;
	lc_lock(&pending_lock);
	for (int i = 0; i < LC_MAX_PENDING; i++) {
		if (pending[i].id == 0) {
			id = next_id++;
			if (next_id == 0) next_id = 1;
			pending[i].id = id;
			pending[i].callback = callback;
			pending[i].data = callback_data;
			break;
		}
	}
	lc_unlock(&pending_lock);
	if (id == 0) {
		/* Full: tell rcheevos now, as a failure worth retrying. */
		rc_api_server_response_t r = { NULL, 0, RC_API_SERVER_RESPONSE_RETRYABLE_CLIENT_ERROR };
		callback(&r, callback_data);
		return;
	}
	on_request(id, request->url, request->post_data, request->content_type);
}

LC_API void lc_free_achievements(void);

static void flatten(const rc_client_event_t* e, lc_event_t* out) {
	memset(out, 0, sizeof *out);
	out->type = e->type;
	if (e->achievement) {
		out->id = e->achievement->id;
		out->points = e->achievement->points;
		out->percent = e->achievement->measured_percent;
		out->title = e->achievement->title;
		out->description = e->achievement->description;
		out->badge = e->achievement->badge_name;
		out->text = e->achievement->measured_progress;
	}
	if (e->leaderboard) {
		out->id = e->leaderboard->id;
		out->title = e->leaderboard->title;
		out->description = e->leaderboard->description;
		out->text = e->leaderboard->tracker_value;
	}
	if (e->leaderboard_tracker) {
		out->id = e->leaderboard_tracker->id;
		out->text = e->leaderboard_tracker->display;
	}
	if (e->leaderboard_scoreboard) {
		out->id = e->leaderboard_scoreboard->leaderboard_id;
		out->points = e->leaderboard_scoreboard->new_rank;
		out->text = e->leaderboard_scoreboard->submitted_score;
	}
	if (e->server_error) {
		out->id = e->server_error->related_id;
		out->title = e->server_error->api;
		out->text = e->server_error->error_message;
	}
	if (e->subset) {
		out->id = e->subset->id;
		out->title = e->subset->title;
	}
}

static void RC_CCONV event_handler(const rc_client_event_t* e, rc_client_t* c) {
	(void)c;
	lc_event_t out;
	flatten(e, &out);
	on_event(&out);
}

static void RC_CCONV log_message(const char* message, const rc_client_t* c) {
	(void)c;
	on_log(message);
}

static void RC_CCONV login_done(int result, const char* error, rc_client_t* c, void* userdata) {
	(void)c;
	(void)userdata;
	on_done(LC_OP_LOGIN, result, error);
}

static void RC_CCONV load_done(int result, const char* error, rc_client_t* c, void* userdata) {
	(void)c;
	(void)userdata;
	on_done(LC_OP_LOAD_GAME, result, error);
}

LC_API uint32_t lc_interface_version(void) { return LC_INTERFACE_VERSION; }

LC_API int lc_init(lc_request_fn request, lc_event_fn event, lc_log_fn log, lc_done_fn done) {
	if (client || !request || !event || !log || !done) return -1;
	ensure_lock();
	on_request = request;
	on_event = event;
	on_log = log;
	on_done = done;
	client = rc_client_create(read_memory, server_call);
	if (!client) return -1;
	rc_client_enable_logging(client, RC_CLIENT_LOG_LEVEL_INFO, log_message);
	rc_client_set_event_handler(client, event_handler);
	/* Casual first: hardcore is RetroAchievements' to grant (ADR 0112). */
	rc_client_set_hardcore_enabled(client, 0);
	return 0;
}

LC_API void lc_shutdown(void) {
	if (!client) return;
	lc_free_achievements();
	lc_lock(&pending_lock);
	memset(pending, 0, sizeof pending);
	lc_unlock(&pending_lock);
	rc_client_destroy(client);
	client = NULL;
	rc_libretro_memory_destroy(&regions);
	memset(&regions, 0, sizeof regions);
}

LC_API size_t lc_user_agent_clause(char* buffer, size_t size) {
	if (!client || !buffer || size == 0) return 0;
	return rc_client_get_user_agent_clause(client, buffer, size);
}

LC_API void lc_server_response(uint32_t id, const char* body, size_t length, int status) {
	rc_client_server_callback_t callback = NULL;
	void* data = NULL;
	if (!lock_ready || id == 0) return;
	lc_lock(&pending_lock);
	for (int i = 0; i < LC_MAX_PENDING; i++) {
		if (pending[i].id == id) {
			callback = pending[i].callback;
			data = pending[i].data;
			pending[i].id = 0;
			break;
		}
	}
	lc_unlock(&pending_lock);
	if (!callback || !client) return;
	rc_api_server_response_t r = { body, length, status };
	callback(&r, data);
}

LC_API void lc_login_with_token(const char* username, const char* token) {
	if (client) rc_client_begin_login_with_token(client, username, token, login_done, NULL);
}

LC_API int lc_set_memory(uint32_t console_id, const void* mmap,
	uint8_t* system_ram, size_t system_ram_size, uint8_t* save_ram, size_t save_ram_size) {
	rc_libretro_memory_destroy(&regions);
	memset(&regions, 0, sizeof regions);
	core_ram[0].data = system_ram;
	core_ram[0].size = system_ram ? system_ram_size : 0;
	core_ram[1].data = save_ram;
	core_ram[1].size = save_ram ? save_ram_size : 0;
	return rc_libretro_memory_init(&regions, (const struct retro_memory_map*)mmap, core_memory_info, console_id);
}

LC_API void lc_load_game(uint32_t console_id, const char* path, const uint8_t* data, size_t size) {
	if (client) rc_client_begin_identify_and_load_game(client, console_id, path, data, size, load_done, NULL);
}

/*
 * One frame first: rcheevos 12.5 crashed in rc_client_raise_mastery_event when
 * a game was unloaded straight after its last achievement unlocked, reading a
 * game it had already cleared. The frame delivers the pending event (and the
 * person hears that they completed the set). rcheevos itself is unpatched.
 */
LC_API void lc_unload_game(void) {
	if (!client) return;
	lc_free_achievements();
	if (rc_client_is_game_loaded(client)) rc_client_do_frame(client);
	rc_client_unload_game(client);
}

LC_API void lc_do_frame(void) {
	if (client) rc_client_do_frame(client);
}

LC_API void lc_idle(void) {
	if (client) rc_client_idle(client);
}

LC_API void lc_reset(void) {
	if (client) rc_client_reset(client);
}

LC_API size_t lc_progress_size(void) { return client ? rc_client_progress_size(client) : 0; }

LC_API int lc_serialize_progress(uint8_t* buffer, size_t size) {
	return client ? rc_client_serialize_progress_sized(client, buffer, size) : -1;
}

LC_API int lc_deserialize_progress(const uint8_t* buffer, size_t size) {
	return client ? rc_client_deserialize_progress_sized(client, buffer, size) : -1;
}

LC_API int lc_hash(uint32_t console_id, const char* path, const uint8_t* data, size_t size, char* out) {
	if (!out) return 0;
	out[0] = '\0';
	if (data && size) return rc_hash_generate_from_buffer(out, console_id, data, size);
	if (path) return rc_hash_generate_from_file(out, console_id, path);
	return 0;
}

LC_API uint32_t lc_memory_regions(void) { return regions.count; }

LC_API uint32_t lc_read_memory(uint32_t address, uint8_t* buffer, uint32_t size) {
	return rc_libretro_memory_read(&regions, address, buffer, size);
}

/* The loaded game, for the menu (interface version 2). */

static void copy_text(char* out, size_t size, const char* s) {
	if (!out || size == 0) return;
	out[0] = '\0';
	if (!s) return;
	strncpy(out, s, size - 1);
	out[size - 1] = '\0';
}

LC_API uint32_t lc_game(char* title, size_t title_size, char* badge, size_t badge_size, lc_summary_t* summary) {
	const rc_client_game_t* game = client ? rc_client_get_game_info(client) : NULL;
	copy_text(title, title_size, NULL);
	copy_text(badge, badge_size, NULL);
	if (summary) memset(summary, 0, sizeof *summary);
	if (!game || game->id == 0) return 0;
	copy_text(title, title_size, game->title);
	copy_text(badge, badge_size, game->badge_name);
	if (summary) {
		rc_client_user_game_summary_t s;
		memset(&s, 0, sizeof s);
		rc_client_get_user_game_summary(client, &s);
		summary->total = s.num_core_achievements;
		summary->unlocked = s.num_unlocked_achievements;
		summary->points = s.points_core;
		summary->points_unlocked = s.points_unlocked;
	}
	return game->id;
}

static rc_client_achievement_list_t* listed;
static uint32_t listed_count;

LC_API void lc_free_achievements(void) {
	if (listed) rc_client_destroy_achievement_list(listed);
	listed = NULL;
	listed_count = 0;
}

LC_API uint32_t lc_list_achievements(void) {
	lc_free_achievements();
	if (!client) return 0;
	listed = rc_client_create_achievement_list(client, RC_CLIENT_ACHIEVEMENT_CATEGORY_CORE,
		RC_CLIENT_ACHIEVEMENT_LIST_GROUPING_LOCK_STATE);
	if (!listed) return 0;
	for (uint32_t b = 0; b < listed->num_buckets; b++) listed_count += listed->buckets[b].num_achievements;
	return listed_count;
}

LC_API int lc_achievement_at(uint32_t index, lc_achievement_t* out) {
	if (!listed || !out || index >= listed_count) return 0;
	for (uint32_t b = 0; b < listed->num_buckets; b++) {
		const rc_client_achievement_bucket_t* bucket = &listed->buckets[b];
		if (index >= bucket->num_achievements) {
			index -= bucket->num_achievements;
			continue;
		}
		const rc_client_achievement_t* a = bucket->achievements[index];
		memset(out, 0, sizeof *out);
		out->id = a->id;
		out->points = a->points;
		out->percent = a->measured_percent;
		out->state = a->unlocked != RC_CLIENT_ACHIEVEMENT_UNLOCKED_NONE ? LC_ACHIEVEMENT_UNLOCKED : LC_ACHIEVEMENT_LOCKED;
		if (bucket->bucket_type == RC_CLIENT_ACHIEVEMENT_BUCKET_UNSUPPORTED) out->state = LC_ACHIEVEMENT_UNSUPPORTED;
		out->title = a->title;
		out->description = a->description;
		out->badge = a->badge_name;
		out->progress = a->measured_progress;
		return 1;
	}
	return 0;
}

LC_API size_t lc_rich_presence(char* buffer, size_t size) {
	if (!client || !buffer || size == 0 || !rc_client_has_rich_presence(client)) return 0;
	return rc_client_get_rich_presence_message(client, buffer, size);
}
