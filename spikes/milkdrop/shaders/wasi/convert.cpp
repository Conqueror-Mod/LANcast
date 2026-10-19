// SPIKE (ADR 0091 step g). milkdrop-shader-converter 0.0.8 as a WASI command.
//
// This is src/main.cpp of jberg/milkdrop-shader-converter @ 291c885 with the
// Node binding taken off and nothing else changed: the same hlsl2glslfork
// calls, the same GLSL ES 3.00 header, the same glsl-optimizer pass. Its
// exceptions become exit codes, because wazero has no exception handling
// and -fno-exceptions keeps the module free of it.
//
//   stdin   one HLSL fragment shader, already wrapped by milkdrop-preset-utils'
//           prepareShader() and renamed shader_body -> xlat_main
//   stdout  the optimised GLSL (what convertHLSLString returned)
//   stderr  the info log on failure
//   exit    0 ok, 1 HLSL parse error, 2 translation error, 3 non-ASCII
//           output, 4 glsl-optimizer refused it, 5 empty input
//
// argv[1] == "raw" skips glsl-optimizer, as convertHLSLString(s, false) does.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <string>

#include "hlsl2glslfork/include/hlsl2glsl.h"
#include "glsl-optimizer/src/glsl/glsl_optimizer.h"

static std::string GetCompiledShaderText(ShHandle parser)
{
	std::string txt = Hlsl2Glsl_GetShader(parser);

	int count = Hlsl2Glsl_GetUniformCount(parser);
	if (count > 0) {
		const ShUniformInfo *uni = Hlsl2Glsl_GetUniformInfo(parser);
		txt += "\n// uniforms:\n";
		for (int i = 0; i < count; ++i) {
			char buf[1000];
			snprintf(buf, 1000, "// %s:%s type %d arrsize %d\n", uni[i].name,
				uni[i].semantic ? uni[i].semantic : "<none>", uni[i].type, uni[i].arraySize);
			txt += buf;
		}
	}
	return txt;
}

static int ConvertString(const std::string &shaderStr, const char *entryPoint,
	ETargetVersion version, unsigned options, std::string &text)
{
	ShHandle parser = Hlsl2Glsl_ConstructCompiler(EShLangFragment);

	if (!Hlsl2Glsl_Parse(parser, shaderStr.c_str(), version, NULL, options)) {
		fprintf(stderr, "HLSL parse error:%s", Hlsl2Glsl_GetInfoLog(parser));
		return 1;
	}

	static EAttribSemantic kAttribSemantic[] = { EAttrSemTangent };
	static const char *kAttribString[] = { "TANGENT" };
	Hlsl2Glsl_SetUserAttributeNames(parser, kAttribSemantic, kAttribString, 1);

	if (!Hlsl2Glsl_Translate(parser, entryPoint, version, options)) {
		fprintf(stderr, "HLSL translation error:%s", Hlsl2Glsl_GetInfoLog(parser));
		return 2;
	}

	text = GetCompiledShaderText(parser);
	for (size_t i = 0, n = text.size(); i != n; ++i) {
		if (!isascii(text[i])) {
			fprintf(stderr, "Contains non-ascii character: %c", text[i]);
			return 3;
		}
	}

	std::string newSrc;
	newSrc.reserve(text.size());
	newSrc += "#version 300 es\n";
	newSrc += "#define lowp\n";
	newSrc += "#define mediump\n";
	newSrc += "#define highp\n";
	newSrc += "#define gl_Vertex _glesVertex\n";
	newSrc += "#define gl_Normal _glesNormal\n";
	newSrc += "#define gl_Color _glesColor\n";
	newSrc += "#define gl_MultiTexCoord0 _glesMultiTexCoord0\n";
	newSrc += "#define gl_MultiTexCoord1 _glesMultiTexCoord1\n";
	newSrc += "#define gl_MultiTexCoord2 _glesMultiTexCoord2\n";
	newSrc += "#define gl_MultiTexCoord3 _glesMultiTexCoord3\n";
	newSrc += "in highp vec4 _glesVertex;\n";
	newSrc += "in highp vec3 _glesNormal;\n";
	newSrc += "in lowp vec4 _glesColor;\n";
	newSrc += "in highp vec4 _glesMultiTexCoord0;\n";
	newSrc += "in highp vec4 _glesMultiTexCoord1;\n";
	newSrc += "in highp vec4 _glesMultiTexCoord2;\n";
	newSrc += "in highp vec4 _glesMultiTexCoord3;\n";
	newSrc += "#define gl_FragData _glesFragData\n";
	newSrc += "out lowp vec4 _glesFragData[4];\n";
	newSrc += text;
	text = newSrc;

	Hlsl2Glsl_DestructCompiler(parser);
	return 0;
}

int main(int argc, char **argv)
{
	bool optimize = !(argc > 1 && strcmp(argv[1], "raw") == 0);

	std::string in;
	char buf[65536];
	size_t n;
	while ((n = fread(buf, 1, sizeof buf, stdin)) > 0)
		in.append(buf, n);
	if (in.empty())
		return 5;

	Hlsl2Glsl_Initialize();

	std::string text;
	int rc = ConvertString(in, "main", ETargetGLSL_ES_300, ETranslateOpNone, text);
	if (rc != 0)
		return rc;

	if (!optimize) {
		fwrite(text.data(), 1, text.size(), stdout);
		return 0;
	}

	glslopt_ctx *ctx = glslopt_initialize(kGlslTargetOpenGLES30);
	glslopt_shader *shader = glslopt_optimize(ctx, kGlslOptShaderFragment, text.c_str(), 0);
	if (!glslopt_get_status(shader)) {
		fprintf(stderr, "glsl-optimizer: %s", glslopt_get_log(shader));
		return 4;
	}
	const char *out = glslopt_get_output(shader);
	fwrite(out, 1, strlen(out), stdout);
	glslopt_shader_delete(shader);
	glslopt_cleanup(ctx);
	Hlsl2Glsl_Shutdown();
	return 0;
}
