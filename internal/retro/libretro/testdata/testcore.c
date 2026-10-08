/*
 * A libretro core that exists to test the host's binding (ADR 0073).
 *
 * It emulates nothing. It asks the questions a real core asks, in the order a
 * real core asks them, and makes every answer visible: the pixel format it was
 * granted, the option value it read, the button it saw, the bytes it was
 * loaded with. Built by the binding's test with whatever gcc is on the PATH;
 * the test skips where there is none, so CI on Linux never needs it.
 */
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

#define EXPORT __declspec(dllexport)

struct retro_system_info { const char *library_name, *library_version, *valid_extensions; bool need_fullpath, block_extract; };
struct retro_game_geometry { unsigned base_width, base_height, max_width, max_height; float aspect_ratio; };
struct retro_system_timing { double fps, sample_rate; };
struct retro_system_av_info { struct retro_game_geometry geometry; struct retro_system_timing timing; };
struct retro_game_info { const char *path; const void *data; size_t size; const char *meta; };
struct retro_variable { const char *key, *value; };

typedef bool (*env_t)(unsigned, void *);
typedef void (*video_t)(const void *, unsigned, unsigned, size_t);
typedef void (*sample_t)(int16_t, int16_t);
typedef size_t (*batch_t)(const int16_t *, size_t);
typedef void (*poll_t)(void);
typedef int16_t (*state_t)(unsigned, unsigned, unsigned, unsigned);

static env_t env; static video_t video; static sample_t sample; static batch_t batch;
static poll_t poll_cb; static state_t state;

static uint32_t frame[4 * 2];
static uint8_t sram[16];
static uint32_t counter;
static uint8_t seed;
static char sysdir[260];
static bool can_dupe;

EXPORT unsigned retro_api_version(void) { return 1; }
EXPORT void retro_set_environment(env_t cb) { env = cb; }
EXPORT void retro_set_video_refresh(video_t cb) { video = cb; }
EXPORT void retro_set_audio_sample(sample_t cb) { sample = cb; }
EXPORT void retro_set_audio_sample_batch(batch_t cb) { batch = cb; }
EXPORT void retro_set_input_poll(poll_t cb) { poll_cb = cb; }
EXPORT void retro_set_input_state(state_t cb) { state = cb; }

EXPORT void retro_get_system_info(struct retro_system_info *info) {
	info->library_name = "lancast-test";
	info->library_version = "1.0";
	info->valid_extensions = "tst|bin";
	info->need_fullpath = false;
	info->block_extract = false;
}

EXPORT void retro_init(void) {
	unsigned fmt = 1; /* XRGB8888 */
	env(10, &fmt);
	const char *dir = NULL;
	if (env(9, &dir) && dir) {
		strncpy(sysdir, dir, sizeof sysdir - 1);
	}
	env(3, &can_dupe);
	static const struct retro_variable vars[] = {
		{ "test_colour", "Colour; red|green|blue" },
		{ "test_speed", "Speed; normal|fast" },
		{ NULL, NULL },
	};
	env(16, (void *)vars);
}

EXPORT void retro_deinit(void) {}

EXPORT void retro_get_system_av_info(struct retro_system_av_info *av) {
	av->geometry.base_width = 4;
	av->geometry.base_height = 2;
	av->geometry.max_width = 4;
	av->geometry.max_height = 2;
	av->geometry.aspect_ratio = 2.0f;
	av->timing.fps = 60.0;
	av->timing.sample_rate = 32768.0;
}

EXPORT void retro_set_controller_port_device(unsigned port, unsigned device) { (void)port; (void)device; }
EXPORT void retro_reset(void) { counter = 0; }

EXPORT bool retro_load_game(const struct retro_game_info *game) {
	if (!game || !game->data || game->size == 0) return false;
	seed = ((const uint8_t *)game->data)[0];
	sram[15] = (uint8_t)game->size;
	return true;
}

EXPORT void retro_unload_game(void) {}

EXPORT void retro_run(void) {
	poll_cb();
	struct retro_variable v = { "test_colour", NULL };
	uint32_t colour = 0x00FF0000; /* red */
	if (env(15, &v) && v.value) {
		if (strcmp(v.value, "green") == 0) colour = 0x0000FF00;
		if (strcmp(v.value, "blue") == 0) colour = 0x000000FF;
	}
	for (int i = 0; i < 8; i++) frame[i] = colour;
	frame[7] = counter;
	if (state(0, 1, 0, 8)) sram[1] = 1; /* RetroPad A */
	sram[0] = (uint8_t)counter;
	sram[2] = seed;
	int16_t samples[4] = { (int16_t)counter, (int16_t)-counter, 7, -7 };
	batch(samples, 2);
	/* Every third frame is a repeat, to prove the host accepts NULL. */
	if (can_dupe && counter % 3 == 2) video(NULL, 4, 2, 16);
	else video(frame, 4, 2, 16);
	counter++;
}

EXPORT size_t retro_serialize_size(void) { return 4; }
EXPORT bool retro_serialize(void *data, size_t size) {
	if (size < 4) return false;
	memcpy(data, &counter, 4);
	return true;
}
EXPORT bool retro_unserialize(const void *data, size_t size) {
	if (size < 4) return false;
	memcpy(&counter, data, 4);
	return true;
}

EXPORT void *retro_get_memory_data(unsigned id) { return id == 0 ? sram : NULL; }
EXPORT size_t retro_get_memory_size(unsigned id) { return id == 0 ? sizeof sram : 0; }
