/*
 * lancast-cheevos.dll: rcheevos behind a small, flat interface the desktop
 * player loads through syscall (ADR 0112).
 *
 * Why a shim at all: rcheevos reads game memory many times a frame. Each read
 * through a Go callback would cross the syscall boundary, and Go allows only
 * about 2,000 callbacks per process. So the memory reads stay in C, against
 * regions registered once per game, and only rare things cross into Go: an
 * HTTP request, an event, a log line and a completion. Each of those is one
 * callback, made once per process.
 *
 * One client per process. The desktop player runs one game at a time, on one
 * thread (internal/retro/host), so a global client is the honest model.
 */
#ifndef LANCAST_CHEEVOS_H
#define LANCAST_CHEEVOS_H

#include <stddef.h>
#include <stdint.h>

#ifdef _WIN32
#define LC_API __declspec(dllexport)
#else
#define LC_API __attribute__((visibility("default")))
#endif

#ifdef __cplusplus
extern "C" {
#endif

/* The version of this interface. Go refuses a DLL with another one. */
#define LC_INTERFACE_VERSION 1

/* An event, flattened from rc_client_event_t so Go reads one fixed layout. */
typedef struct lc_event_t {
	uint32_t type;        /* RC_CLIENT_EVENT_* */
	uint32_t id;          /* achievement, leaderboard or tracker id */
	uint32_t points;      /* an achievement's points */
	float percent;        /* an achievement's measured percent */
	const char* title;    /* achievement or leaderboard title */
	const char* description;
	const char* badge;    /* an achievement's badge name, "12345" */
	const char* text;     /* progress, tracker display, score, or error */
} lc_event_t;

/* Operations whose completion is reported through lc_done_fn. */
enum { LC_OP_LOGIN = 1, LC_OP_LOAD_GAME = 2 };

/* rcheevos wants an HTTP call made. Answer with lc_server_response(id, …),
 * from any thread, once. post_data is NULL for a GET. */
typedef void (*lc_request_fn)(uint32_t id, const char* url, const char* post_data, const char* content_type);
typedef void (*lc_event_fn)(const lc_event_t* event);
typedef void (*lc_log_fn)(const char* message);
/* result is RC_OK (0) on success; error is NULL then. */
typedef void (*lc_done_fn)(uint32_t op, int result, const char* error);

LC_API uint32_t lc_interface_version(void);

/* lc_init creates the client, casual (hardcore off; ADR 0112 section 5). It
 * may be called again after lc_shutdown. Returns 0 on success. */
LC_API int lc_init(lc_request_fn request, lc_event_fn event, lc_log_fn log, lc_done_fn done);
/* lc_shutdown destroys the client. Answers still in flight are dropped. */
LC_API void lc_shutdown(void);

/* "rcheevos/12.5.0", for the end of the HTTP user agent. */
LC_API size_t lc_user_agent_clause(char* buffer, size_t size);

/* An answer to request id. status is the HTTP status, or -2 for a failure
 * worth retrying (no connection) and -1 for one that isn't. */
LC_API void lc_server_response(uint32_t id, const char* body, size_t length, int status);

LC_API void lc_login_with_token(const char* username, const char* token);

/*
 * lc_set_memory registers the game's memory, before lc_load_game. mmap is a
 * struct retro_memory_map, or NULL for a core that sent none; system and save
 * RAM are the core's retro_get_memory_data blocks, used when the map has no
 * region for an address. Read only during lc_do_frame and lc_idle.
 * Returns 1 if any memory was found.
 */
LC_API int lc_set_memory(uint32_t console_id, const void* mmap,
	uint8_t* system_ram, size_t system_ram_size, uint8_t* save_ram, size_t save_ram_size);

/* lc_load_game hashes the game and loads its achievements. Pass the bytes, or
 * a path for a game too big to hold (data NULL). */
LC_API void lc_load_game(uint32_t console_id, const char* path, const uint8_t* data, size_t size);
LC_API void lc_unload_game(void);

/* Once per emulated frame, and lc_idle at least once a second while paused. */
LC_API void lc_do_frame(void);
LC_API void lc_idle(void);
/* After the game is reset, and when RC_CLIENT_EVENT_RESET asks for one. */
LC_API void lc_reset(void);

/* Progress inside a save state (ADR 0112 section 7). */
LC_API size_t lc_progress_size(void);
LC_API int lc_serialize_progress(uint8_t* buffer, size_t size);
/* NULL restores "no progress", for a state saved without it. */
LC_API int lc_deserialize_progress(const uint8_t* buffer, size_t size);

/* RetroAchievements' hash of a game, into out[33]. Returns 1 on success. */
LC_API int lc_hash(uint32_t console_id, const char* path, const uint8_t* data, size_t size, char* out);

/* How many regions lc_set_memory found, and one byte read through them, for
 * tests: proves an address resolves through the map. Returns bytes read. */
LC_API uint32_t lc_memory_regions(void);
LC_API uint32_t lc_read_memory(uint32_t address, uint8_t* buffer, uint32_t size);

#ifdef __cplusplus
}
#endif

#endif
