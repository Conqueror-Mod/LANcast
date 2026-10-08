/*
 * A libretro core that renders through OpenGL, to test the host's hardware
 * path (ADR 0073, stage 3). It asks for a 3.3 core-profile context, as
 * Mupen64Plus-Next's GLideN64 does, resolves every GL function through the
 * host's get_proc_address, clears the host's framebuffer to green, and hands
 * the frame over as RETRO_HW_FRAME_BUFFER_VALID. Its save RAM reports what
 * happened: context resets, destroys, frames drawn.
 */
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

#define EXPORT __declspec(dllexport)
#define APIENTRY __stdcall

struct retro_system_info { const char *library_name, *library_version, *valid_extensions; bool need_fullpath, block_extract; };
struct retro_game_geometry { unsigned base_width, base_height, max_width, max_height; float aspect_ratio; };
struct retro_system_timing { double fps, sample_rate; };
struct retro_system_av_info { struct retro_game_geometry geometry; struct retro_system_timing timing; };
struct retro_game_info { const char *path; const void *data; size_t size; const char *meta; };

typedef void (*reset_t)(void);
typedef uintptr_t (*get_fb_t)(void);
typedef void (*(*get_proc_t)(const char *))(void);
struct retro_hw_render_callback {
	int context_type;
	reset_t context_reset;
	get_fb_t get_current_framebuffer;
	get_proc_t get_proc_address;
	bool depth, stencil, bottom_left_origin;
	unsigned version_major, version_minor;
	bool cache_context;
	reset_t context_destroy;
	bool debug_context;
};

typedef bool (*env_t)(unsigned, void *);
typedef void (*video_t)(const void *, unsigned, unsigned, size_t);
typedef void (*sample_t)(int16_t, int16_t);
typedef size_t (*batch_t)(const int16_t *, size_t);
typedef void (*poll_t)(void);
typedef int16_t (*state_t)(unsigned, unsigned, unsigned, unsigned);

typedef void (APIENTRY *glClearColor_t)(float, float, float, float);
typedef void (APIENTRY *glClear_t)(unsigned);
typedef void (APIENTRY *glViewport_t)(int, int, int, int);
typedef void (APIENTRY *glBindFramebuffer_t)(unsigned, unsigned);

static env_t env; static video_t video; static batch_t batch; static poll_t poll_cb;
static struct retro_hw_render_callback hw;
static glClearColor_t pClearColor; static glClear_t pClear; static glViewport_t pViewport;
static glBindFramebuffer_t pBindFramebuffer;
static uint8_t sram[16]; /* [0] resets, [1] destroys, [2] frames drawn, [3] functions missing */

EXPORT unsigned retro_api_version(void) { return 1; }
EXPORT void retro_set_environment(env_t cb) { env = cb; }
EXPORT void retro_set_video_refresh(video_t cb) { video = cb; }
EXPORT void retro_set_audio_sample(sample_t cb) { (void)cb; }
EXPORT void retro_set_audio_sample_batch(batch_t cb) { batch = cb; }
EXPORT void retro_set_input_poll(poll_t cb) { poll_cb = cb; }
EXPORT void retro_set_input_state(state_t cb) { (void)cb; }
EXPORT void retro_get_system_info(struct retro_system_info *info) {
	info->library_name = "lancast-gltest"; info->library_version = "1.0";
	info->valid_extensions = "tst"; info->need_fullpath = false; info->block_extract = false;
}
EXPORT void retro_init(void) {}
EXPORT void retro_deinit(void) {}
EXPORT void retro_get_system_av_info(struct retro_system_av_info *av) {
	av->geometry.base_width = 64; av->geometry.base_height = 32;
	av->geometry.max_width = 64; av->geometry.max_height = 32;
	av->geometry.aspect_ratio = 2.0f;
	av->timing.fps = 60.0; av->timing.sample_rate = 32000.0;
}
EXPORT void retro_set_controller_port_device(unsigned p, unsigned d) { (void)p; (void)d; }
EXPORT void retro_reset(void) {}

static void context_reset(void) {
	pClearColor = (glClearColor_t)hw.get_proc_address("glClearColor");
	pClear = (glClear_t)hw.get_proc_address("glClear");
	pViewport = (glViewport_t)hw.get_proc_address("glViewport");
	pBindFramebuffer = (glBindFramebuffer_t)hw.get_proc_address("glBindFramebuffer");
	sram[3] = (!pClearColor) + (!pClear) + (!pViewport) + (!pBindFramebuffer);
	sram[0]++;
}
static void context_destroy(void) { sram[1]++; }

EXPORT bool retro_load_game(const struct retro_game_info *game) {
	(void)game;
	memset(&hw, 0, sizeof hw);
	hw.context_type = 3; /* OPENGL_CORE */
	hw.version_major = 3; hw.version_minor = 3;
	hw.depth = true; hw.stencil = true; hw.bottom_left_origin = true;
	hw.context_reset = context_reset;
	hw.context_destroy = context_destroy;
	return env(14, &hw);
}
EXPORT void retro_unload_game(void) {}

EXPORT void retro_run(void) {
	poll_cb();
	if (sram[0] && !sram[3]) {
		pBindFramebuffer(0x8D40, (unsigned)hw.get_current_framebuffer());
		pViewport(0, 0, 64, 32);
		pClearColor(0.0f, 1.0f, 0.0f, 1.0f);
		pClear(0x4000);
		sram[2]++;
	}
	int16_t s[2] = {0, 0};
	batch(s, 1);
	video((const void *)(uintptr_t)-1, 64, 32, 0);
}

EXPORT size_t retro_serialize_size(void) { return 0; }
EXPORT bool retro_serialize(void *d, size_t n) { (void)d; (void)n; return false; }
EXPORT bool retro_unserialize(const void *d, size_t n) { (void)d; (void)n; return false; }
EXPORT void *retro_get_memory_data(unsigned id) { return id == 0 ? sram : NULL; }
EXPORT size_t retro_get_memory_size(unsigned id) { return id == 0 ? sizeof sram : 0; }
