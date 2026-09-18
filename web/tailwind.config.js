/** @type {import('tailwindcss').Config} */
export default {
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    // opacity 是顶级覆盖（不是 extend），白名单外的档位会静默失效。
    // 35 / 55 / 70 是代码里已经在用但改版前一直没生效的三档，这里补齐。
    opacity: {
      0: "0",
      5: "0.05",
      8: "0.08",
      10: "0.1",
      12: "0.12",
      15: "0.15",
      20: "0.2",
      25: "0.25",
      30: "0.3",
      35: "0.35",
      40: "0.4",
      45: "0.45",
      50: "0.5",
      55: "0.55",
      60: "0.6",
      65: "0.65",
      70: "0.7",
      75: "0.75",
      80: "0.8",
      90: "0.9",
      96: "0.96",
      100: "1",
    },
    extend: {
      fontFamily: {
        // 这套语言没有衬线角色：正文与 chrome 都是 Geist Variable。
        // serif / display 保留为 sans 的别名，散落的类名不至于漏出体系外。
        sans: ["var(--font-sans)"],
        serif: ["var(--font-sans)"],
        display: ["var(--font-sans)"],
        // 一切数字、编号、徽章走等宽 —— 「数字即仪表」。
        mono: ["var(--font-mono)"],
      },
      colors: {
        /* ---- 规范原生 token（具名，供新代码直接引用） ---- */
        bg: "rgb(var(--bg) / <alpha-value>)",
        panel: "rgb(var(--panel) / <alpha-value>)",
        raised: "rgb(var(--raised) / <alpha-value>)",
        muted: "rgb(var(--muted) / <alpha-value>)",
        faint: "rgb(var(--faint) / <alpha-value>)",
        "line-soft": "rgb(var(--line-soft) / <alpha-value>)",
        amber: {
          DEFAULT: "rgb(var(--amber) / <alpha-value>)",
          soft: "rgb(var(--amber-soft) / <alpha-value>)",
          ink: "rgb(var(--accent-ink) / <alpha-value>)",
        },
        pos: "rgb(var(--pos) / <alpha-value>)",
        neg: "rgb(var(--neg) / <alpha-value>)",
        warn: "rgb(var(--warn) / <alpha-value>)",
        /* 图表序列色 */
        s1: "rgb(var(--s1) / <alpha-value>)",
        s2: "rgb(var(--s2) / <alpha-value>)",
        s3: "rgb(var(--s3) / <alpha-value>)",
        s4: "rgb(var(--s4) / <alpha-value>)",
        s5: "rgb(var(--s5) / <alpha-value>)",

        /* ---- 语义层：组件只认这一组 ---- */
        surface: {
          DEFAULT: "rgb(var(--surface) / <alpha-value>)",
          subtle: "rgb(var(--surface-subtle) / <alpha-value>)",
          muted: "rgb(var(--surface-muted) / <alpha-value>)",
          raised: "rgb(var(--surface-raised) / <alpha-value>)",
        },
        canvas: "rgb(var(--canvas) / <alpha-value>)",
        sidebar: "rgb(var(--sidebar) / <alpha-value>)",
        line: "rgb(var(--line) / <alpha-value>)",
        ink: {
          DEFAULT: "rgb(var(--ink) / <alpha-value>)",
          muted: "rgb(var(--ink-muted) / <alpha-value>)",
          faint: "rgb(var(--ink-faint) / <alpha-value>)",
        },
        /* accent = 墨色：高频状态色，不消耗强调色 */
        accent: {
          DEFAULT: "rgb(var(--accent) / <alpha-value>)",
          soft: "rgb(var(--accent-soft) / <alpha-value>)",
        },
        /* action = 琥珀橙：全站唯一彩色，只给主 CTA 与最重要的信号 */
        action: {
          DEFAULT: "rgb(var(--action) / <alpha-value>)",
          deep: "rgb(var(--action-deep) / <alpha-value>)",
        },

        /* ---- 旧命名别名：全部重新指向新调色板，保证散落类名不失效 ---- */
        "slate-dark": "rgb(var(--slate-dark) / <alpha-value>)",
        "slate-mid": "rgb(var(--slate-mid) / <alpha-value>)",
        "ivory-medium": "rgb(var(--ivory-medium) / <alpha-value>)",
        "ivory-light": "rgb(var(--ivory-light) / <alpha-value>)",
        "cloud-medium": "rgb(var(--cloud-medium) / <alpha-value>)",
        "cloud-dark": "rgb(var(--cloud-dark) / <alpha-value>)",
        stone: "rgb(var(--stone) / <alpha-value>)",
        "oat-warm": "rgb(var(--oat-warm) / <alpha-value>)",
        manilla: "rgb(var(--manilla) / <alpha-value>)",
        clay: "rgb(var(--clay) / <alpha-value>)",
        "clay-deep": "rgb(var(--clay-deep) / <alpha-value>)",
        "stone-canvas": "rgb(var(--stone-canvas) / <alpha-value>)",
        "pure-white": "rgb(var(--pure-white) / <alpha-value>)",
        "stone-border": "rgb(var(--stone-border) / <alpha-value>)",
        "stone-muted": "rgb(var(--stone-muted) / <alpha-value>)",
        "ash-gray": "rgb(var(--ash-gray) / <alpha-value>)",
        "warm-gray": "rgb(var(--warm-gray) / <alpha-value>)",
        "ink-black": "rgb(var(--ink-black) / <alpha-value>)",
        soot: "rgb(var(--soot) / <alpha-value>)",
        "sky-wash": "rgb(var(--sky-wash) / <alpha-value>)",
        "cyan-signal": "rgb(var(--cyan-signal) / <alpha-value>)",
        "cyan-edge": "rgb(var(--cyan-edge) / <alpha-value>)",
        parchment: "rgb(var(--parchment) / <alpha-value>)",
        "lake-blue": "rgb(var(--lake-blue) / <alpha-value>)",
        "periwinkle-mist": "rgb(var(--periwinkle-mist) / <alpha-value>)",
        "sky-blue": "rgb(var(--sky-blue) / <alpha-value>)",
        mint: "rgb(var(--mint) / <alpha-value>)",
        coral: "rgb(var(--coral) / <alpha-value>)",
        gold: "rgb(var(--gold) / <alpha-value>)",
        crimson: "rgb(var(--crimson) / <alpha-value>)",
        "off-black": "rgb(var(--off-black) / <alpha-value>)",
        graphite: "rgb(var(--graphite) / <alpha-value>)",
        smoke: "rgb(var(--smoke) / <alpha-value>)",
        ash: "rgb(var(--ash) / <alpha-value>)",
        paper: "rgb(var(--paper) / <alpha-value>)",
        "card-mint": "rgb(var(--card-mint) / <alpha-value>)",
        blush: "rgb(var(--blush) / <alpha-value>)",
        "sea-foam": "rgb(var(--sea-foam) / <alpha-value>)",
        "mint-mist": "rgb(var(--mint-mist) / <alpha-value>)",
        "deep-teal": "rgb(var(--deep-teal) / <alpha-value>)",
        pine: "rgb(var(--pine) / <alpha-value>)",
        sage: "rgb(var(--sage) / <alpha-value>)",
        lake: "rgb(var(--lake) / <alpha-value>)",
        forest: "rgb(var(--forest) / <alpha-value>)",
        navy: "rgb(var(--navy) / <alpha-value>)",
        rose: "rgb(var(--rose) / <alpha-value>)",
        charcoal: "rgb(var(--charcoal) / <alpha-value>)",
        "illustration-ink": "rgb(var(--illustration-ink) / <alpha-value>)",

        /* 状态语义：与原生 token 同源，统一走 rgb（原先是 oklch） */
        review: "rgb(var(--review) / <alpha-value>)",
        danger: "rgb(var(--danger) / <alpha-value>)",
        success: "rgb(var(--success) / <alpha-value>)",
        warning: "rgb(var(--warning) / <alpha-value>)",
        info: "rgb(var(--info) / <alpha-value>)",
        neutral: "rgb(var(--neutral) / <alpha-value>)",
        "on-accent": "rgb(var(--on-accent) / <alpha-value>)",
        overlay: "rgb(var(--overlay) / <alpha-value>)",
      },
      // 规范圆角体系：小控件 4px / 面板 6px / 大容器 10px。
      // 默认档位一并压到这三档上，散落的 rounded-md / rounded-lg 不会漏出体系外。
      borderRadius: {
        DEFAULT: "4px",
        sm: "4px",
        md: "6px",
        lg: "10px",
        xl: "10px",
        "2xl": "10px",
        "3xl": "10px",
        card: "6px",
        panel: "6px",
        button: "4px",
        pill: "99px",
        nav: "4px",
        tag: "4px",
      },
      boxShadow: {
        // 铁律一：发丝线，不要投影。保留键名让残留类名不至于报错。
        sm: "none",
        DEFAULT: "none",
        md: "none",
        lg: "none",
        xl: "none",
        "2xl": "none",
      },
      fontSize: {
        // 规范字阶。正文基准 14px / 行高 1.55。
        h1: ["16px", { lineHeight: "1.35", letterSpacing: "-0.015em", fontWeight: "560" }],
        h2: ["14px", { lineHeight: "1.4", letterSpacing: "-0.015em", fontWeight: "550" }],
        body: ["14px", { lineHeight: "1.55" }],
        "body-sm": ["13px", { lineHeight: "1.55" }],
        control: ["12.5px", { lineHeight: "1.4", fontWeight: "500" }],
        eyebrow: ["10.5px", { lineHeight: "1.3", letterSpacing: "0.09em", fontWeight: "500" }],
        th: ["10.5px", { lineHeight: "1.3", letterSpacing: "0.07em", fontWeight: "500" }],
        badge: ["10.5px", { lineHeight: "1.3", fontWeight: "500" }],
        // 旧键名重新落到新字阶，避免营销尺度（61/68px）漏进后台。
        caption: ["12px", { lineHeight: "1.45" }],
        subheading: ["14px", { lineHeight: "1.4", letterSpacing: "-0.015em" }],
        heading: ["16px", { lineHeight: "1.35", letterSpacing: "-0.015em" }],
        display: ["16px", { lineHeight: "1.35", letterSpacing: "-0.015em" }],
      },
      fontWeight: {
        // Geist 是可变字重，规范用到 450 / 520 / 550 / 560 这些中间档。
        normal: "450",
        medium: "500",
        semibold: "550",
        bold: "620",
        nav: "520",
        panel: "550",
        page: "560",
      },
      letterSpacing: {
        eyebrow: "0.09em",
        th: "0.07em",
        tight: "-0.015em",
      },
      transitionTimingFunction: {
        // 规范：全站统一缓动
        std: "cubic-bezier(.22, .7, .2, 1)",
      },
      transitionDuration: {
        fast: "160ms",
        slow: "340ms",
      },
      maxWidth: {
        workbench: "1280px",
      },
      spacing: {
        // 规范：侧栏 198px、面板内边距 20px、卡片间距 14px
        nav: "198px",
        panel: "20px",
        gutter: "14px",
        18: "4.5rem",
        22: "5.5rem",
        76: "76px",
        100: "100px",
      },
      backdropBlur: {
        chrome: "12px",
      },
      keyframes: {
        // 入场：透明度 + 上移 6px
        "enter-up": {
          from: { opacity: "0", transform: "translateY(6px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        // 实时状态点：2.2s 呼吸
        breathe: {
          "0%, 100%": { opacity: "1", transform: "scale(1)" },
          "50%": { opacity: "0.45", transform: "scale(0.86)" },
        },
      },
      animation: {
        "enter-up": "enter-up 340ms cubic-bezier(.22,.7,.2,1) both",
        breathe: "breathe 2.2s cubic-bezier(.22,.7,.2,1) infinite",
      },
    },
  },
  plugins: [],
};
