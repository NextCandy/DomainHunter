import { useEffect, useRef } from "react";

/**
 * 氛围层：视口最底层的一张 WebGL 极光。
 *
 * 规范的约束是「只贡献灯光，不贡献对比度」，所以这里做了三件克制的事：
 *   1. 渲染分辨率固定在 40%，再由 CSS 拉满视口 —— 极光本来就是低频色块，
 *      看不出差别，但片元数只有 16%。
 *   2. 帧率封顶 30fps。
 *   3. 最终颜色只在底色与光色之间取 ~8.5% 的混合，光是「透上来」的，不是画上去的。
 *
 * 颜色全部从 --fx-* token 读，不写死任何色值；主题切换时 MutationObserver
 * 会重新取色，所以明暗两套氛围色能跟着切。
 * WebGL 不可用、着色器编译失败、或用户开了 prefers-reduced-motion 时，
 * 都优雅降级（不渲染 / 只画一帧静态图），页面照常可用。
 */

const VERT = `
attribute vec2 aPos;
void main() { gl_Position = vec4(aPos, 0.0, 1.0); }
`;

const FRAG = `
precision mediump float;
uniform vec2 uRes;
uniform float uTime;
uniform vec3 uBg;
uniform vec3 uC1;
uniform vec3 uC2;
uniform vec3 uC3;
uniform float uAmp;

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453);
}

float vnoise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(hash(i), hash(i + vec2(1.0, 0.0)), u.x),
    mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), u.x),
    u.y
  );
}

float fbm(vec2 p) {
  float v = 0.0;
  float a = 0.5;
  for (int i = 0; i < 4; i++) {
    v += a * vnoise(p);
    p *= 2.03;
    a *= 0.5;
  }
  return v;
}

void main() {
  vec2 uv = gl_FragCoord.xy / uRes;
  vec2 p = uv * vec2(1.7, 1.0);
  float t = uTime * 0.026;

  // 两层错向漂移的极光带，第二层被第一层扰动，避免看出是噪声纹理
  float n1 = fbm(p * 1.7 + vec2(t, t * 0.55));
  float n2 = fbm(p * 2.5 + vec2(-t * 0.75, t * 0.35) + n1 * 0.6);
  float band = smoothstep(0.28, 0.96, n2);

  // 光从上方来：给一个垂直衰减，让底部沉下去
  float fall = smoothstep(1.25, -0.15, uv.y);
  // 四角压暗，避免边缘出现直线接缝
  float vign = smoothstep(1.35, 0.15, length(uv - 0.5) * 1.25);

  vec3 col = uBg;
  col = mix(col, uC1, band * fall * 0.60);
  col = mix(col, uC2, fbm(p * 1.15 - vec2(t * 0.45, 0.0)) * fall * 0.32);
  col = mix(col, uC3, pow(band, 2.2) * fall * 0.45);

  // 只贡献灯光：整体振幅压到 uAmp
  gl_FragColor = vec4(mix(uBg, col, uAmp * vign), 1.0);
}
`;

/** 把 "13 11 10" 这样的 token 值读成 0–1 的 vec3。 */
function readColor(styles: CSSStyleDeclaration, name: string, fallback: [number, number, number]) {
  const raw = styles.getPropertyValue(name).trim();
  if (!raw) return fallback;
  const parts = raw.split(/[\s,]+/).map((v) => Number.parseFloat(v));
  if (parts.length < 3 || parts.some((v) => !Number.isFinite(v))) return fallback;
  return [parts[0] / 255, parts[1] / 255, parts[2] / 255] as [number, number, number];
}

function compile(gl: WebGLRenderingContext, type: number, src: string) {
  const sh = gl.createShader(type);
  if (!sh) return null;
  gl.shaderSource(sh, src);
  gl.compileShader(sh);
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    gl.deleteShader(sh);
    return null;
  }
  return sh;
}

export function AuroraBackground() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const gl = (canvas.getContext("webgl", { alpha: false, antialias: false, depth: false }) ||
      canvas.getContext("experimental-webgl", { alpha: false })) as WebGLRenderingContext | null;
    // 没有 WebGL 就什么都不画，页面靠 body 的 canvas 底色照常显示
    if (!gl) return;

    const vs = compile(gl, gl.VERTEX_SHADER, VERT);
    const fs = compile(gl, gl.FRAGMENT_SHADER, FRAG);
    if (!vs || !fs) return;

    const prog = gl.createProgram();
    if (!prog) return;
    gl.attachShader(prog, vs);
    gl.attachShader(prog, fs);
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      gl.deleteProgram(prog);
      return;
    }
    gl.useProgram(prog);

    const buf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 3, -1, -1, 3]), // 一个覆盖屏幕的大三角
      gl.STATIC_DRAW,
    );
    const aPos = gl.getAttribLocation(prog, "aPos");
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 0, 0);

    const uRes = gl.getUniformLocation(prog, "uRes");
    const uTime = gl.getUniformLocation(prog, "uTime");
    const uBg = gl.getUniformLocation(prog, "uBg");
    const uC1 = gl.getUniformLocation(prog, "uC1");
    const uC2 = gl.getUniformLocation(prog, "uC2");
    const uC3 = gl.getUniformLocation(prog, "uC3");
    const uAmp = gl.getUniformLocation(prog, "uAmp");

    // 从 token 取色；主题切换后重新取
    function syncColors() {
      const st = getComputedStyle(document.documentElement);
      gl!.uniform3fv(uBg, readColor(st, "--fx-bg", [0.05, 0.043, 0.039]));
      gl!.uniform3fv(uC1, readColor(st, "--fx-1", [0.576, 0.239, 0.02]));
      gl!.uniform3fv(uC2, readColor(st, "--fx-2", [0.227, 0.11, 0.031]));
      gl!.uniform3fv(uC3, readColor(st, "--fx-3", [0.361, 0.165, 0.024]));
    }
    syncColors();
    gl.uniform1f(uAmp, 0.085); // 规范：~8% 振幅

    const SCALE = 0.4; // 规范：40% 分辨率
    function resize() {
      const w = Math.max(1, Math.round(window.innerWidth * SCALE));
      const h = Math.max(1, Math.round(window.innerHeight * SCALE));
      if (canvas!.width === w && canvas!.height === h) return;
      canvas!.width = w;
      canvas!.height = h;
      gl!.viewport(0, 0, w, h);
      gl!.uniform2f(uRes, w, h);
    }
    resize();

    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
    let raf = 0;
    let last = 0;
    const FRAME = 1000 / 30; // 规范：30fps 封顶
    const start = performance.now();

    function draw(now: number) {
      raf = requestAnimationFrame(draw);
      if (now - last < FRAME) return;
      last = now;
      resize();
      gl!.uniform1f(uTime, (now - start) / 1000);
      gl!.drawArrays(gl!.TRIANGLES, 0, 3);
    }

    function renderStatic() {
      resize();
      gl!.uniform1f(uTime, 12.0); // 一帧好看的定格
      gl!.drawArrays(gl!.TRIANGLES, 0, 3);
    }

    function startLoop() {
      cancelAnimationFrame(raf);
      if (reduce.matches) renderStatic();
      else raf = requestAnimationFrame(draw);
    }
    startLoop();

    // 标签页不可见时停掉，别白烧 GPU
    function onVisibility() {
      if (document.hidden) cancelAnimationFrame(raf);
      else startLoop();
    }
    document.addEventListener("visibilitychange", onVisibility);
    reduce.addEventListener("change", startLoop);
    window.addEventListener("resize", resize);

    // 主题切换（html 的 class 变化）后重新取色并补一帧
    const mo = new MutationObserver(() => {
      syncColors();
      if (reduce.matches) renderStatic();
    });
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });

    return () => {
      cancelAnimationFrame(raf);
      document.removeEventListener("visibilitychange", onVisibility);
      reduce.removeEventListener("change", startLoop);
      window.removeEventListener("resize", resize);
      mo.disconnect();
      gl.deleteBuffer(buf);
      gl.deleteProgram(prog);
      gl.deleteShader(vs);
      gl.deleteShader(fs);
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    };
  }, []);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className="pointer-events-none fixed inset-0 -z-10 h-full w-full"
    />
  );
}
