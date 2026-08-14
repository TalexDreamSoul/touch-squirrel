"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import Image from "next/image";
import { Button } from "@cloudflare/kumo";
import "./onboarding.css";

const SEEN_KEY = "squirrel-onboarding-v1";
const STRIPS = 18;

export function hasSeenOnboarding(): boolean {
  if (typeof window === "undefined") return true;
  try {
    return localStorage.getItem(SEEN_KEY) === "1";
  } catch {
    return true;
  }
}

export function markOnboardingSeen() {
  try {
    localStorage.setItem(SEEN_KEY, "1");
  } catch {
    /* 隐私模式下写入失败，引导下次仍会出现，可接受 */
  }
}

export function resetOnboarding() {
  try {
    localStorage.removeItem(SEEN_KEY);
  } catch {
    /* ignore */
  }
}

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** 立方体站位：x/y 以立方体边长为单位，z 为像素 */
type Pose = { x: number; y: number; z: number; rx: number; ry: number; k: number; o: number };

const FORMATIONS = {
  // 入场前：散落在下方看不见的地方
  enter: [
    { x: -1.6, y: 4.2, z: -120, rx: -40, ry: 30, k: 0.4, o: 0 },
    { x: 0.2, y: 4.6, z: 40, rx: 25, ry: -35, k: 0.4, o: 0 },
    { x: 1.7, y: 4.3, z: -80, rx: -30, ry: 45, k: 0.4, o: 0 },
    { x: -0.9, y: 4.8, z: 90, rx: 40, ry: -20, k: 0.4, o: 0 },
    { x: 1.1, y: 5.0, z: -40, rx: -20, ry: 40, k: 0.4, o: 0 },
    { x: 0, y: 5.4, z: 120, rx: 35, ry: -45, k: 0.4, o: 0 },
  ],
  // 第 1 步：堆成一座垛，这就是「囤」
  pile: [
    { x: -1.02, y: 1.0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
    { x: 0, y: 1.0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
    { x: 1.02, y: 1.0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
    { x: -0.51, y: 0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
    { x: 0.51, y: 0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
    { x: 0, y: -1.0, z: 0, rx: 0, ry: 0, k: 1, o: 1 },
  ],
  // 第 2 步：摊开分层，这是「分门别类」
  fan: [
    { x: -3.1, y: 0.42, z: -40, rx: -12, ry: 22, k: 0.82, o: 1 },
    { x: -1.86, y: -0.32, z: 30, rx: 14, ry: -18, k: 0.82, o: 1 },
    { x: -0.62, y: 0.38, z: -20, rx: -8, ry: 30, k: 0.82, o: 1 },
    { x: 0.62, y: -0.38, z: 40, rx: 16, ry: -26, k: 0.82, o: 1 },
    { x: 1.86, y: 0.32, z: -30, rx: -14, ry: 20, k: 0.82, o: 1 },
    { x: 3.1, y: -0.28, z: 20, rx: 10, ry: -30, k: 0.82, o: 1 },
  ],
  // 第 3 步：其余整个退出画面，只留品牌立方体
  focus: [
    { x: -1.9, y: 0.9, z: -420, rx: -10, ry: 25, k: 0.7, o: 0 },
    { x: -0.7, y: 1.1, z: -460, rx: 8, ry: -20, k: 0.7, o: 0 },
    { x: 0.7, y: 1.1, z: -460, rx: -8, ry: 20, k: 0.7, o: 0 },
    { x: 1.9, y: 0.9, z: -420, rx: 10, ry: -25, k: 0.7, o: 0 },
    { x: 0, y: 1.3, z: -500, rx: 0, ry: 15, k: 0.7, o: 0 },
    { x: 0, y: -0.1, z: 150, rx: -12, ry: 32, k: 1.35, o: 1 },
  ],
} satisfies Record<string, Pose[]>;

type Formation = keyof typeof FORMATIONS;

/** 持续飞入的货：从画面下方各处飞来，落到垛上被"收进去" */
type Flyer = {
  fx: string;
  fz: string;
  fa: number;
  fb: number;
  tx: number;
  ty: number;
  tz: number;
  size: number;
  dur: number;
  delay: number;
  brand?: boolean;
};

const FLYERS: Flyer[] = [
  { fx: "-320px", fz: "-180px", fa: -50, fb: 40, tx: -1.02, ty: 1.0, tz: 0, size: 0.9, dur: 4.2, delay: 0 },
  { fx: "300px", fz: "140px", fa: 40, fb: -55, tx: 0.51, ty: 0, tz: 0, size: 0.8, dur: 4.6, delay: 0.6 },
  { fx: "-180px", fz: "200px", fa: 55, fb: 30, tx: 0, ty: -1.0, tz: 0, size: 0.85, dur: 4.4, delay: 1.2, brand: true },
  { fx: "380px", fz: "-120px", fa: -35, fb: -40, tx: 1.02, ty: 1.0, tz: 0, size: 0.95, dur: 4.8, delay: 1.8 },
  { fx: "-400px", fz: "60px", fa: 45, fb: 50, tx: -0.51, ty: 0, tz: 0, size: 0.75, dur: 4.3, delay: 2.4 },
  { fx: "140px", fz: "-220px", fa: -55, fb: 35, tx: 0, ty: 1.0, tz: 0, size: 0.88, dur: 4.7, delay: 3.0 },
];

const CUBE_FACES = ["front", "back", "right", "left", "top", "bottom"] as const;

/**
 * 每块货对应一个真实的注册插件（见 plugins/ 目录）。
 * 图标取自 @lobehub/icons-static-svg 的单色版本，用 mask 上色。
 * Outlook 用微软标记，图标集里没有单独的 Outlook 版本。
 */
const SERVICES = [
  "openai",
  "claudecode",
  "github",
  "xai",
  "microsoft",
] as const;

function CubeFaces({ logo, mark }: { logo?: string; mark?: boolean }) {
  return (
    <>
      {CUBE_FACES.map((f) => {
        const showFace = f === "front" || f === "back";
        return (
          <div key={f} className={`sq-ob__face sq-ob__face--${f}`}>
            {mark && showFace ? <span className="sq-ob__cube-mark" /> : null}
            {logo && !mark && showFace ? (
              <span
                className="sq-ob__logo"
                style={
                  { "--logo": `url(/brand/services/${logo}.svg)` } as React.CSSProperties
                }
              />
            ) : null}
          </div>
        );
      })}
    </>
  );
}

type Step = {
  eyebrow: string;
  /** 首屏用品牌标识代替普通眉标 */
  brand?: boolean;
  lines: string[];
  desc: string;
  formation: Formation;
  cta: string;
};

const STEPS: Step[] = [
  {
    eyebrow: "松果仓",
    brand: true,
    lines: ["Hi，遇见你", "囤货美好"],
    desc: "松鼠替你把每一次注册的产物收进同一个仓：不丢、不散、随时取。",
    formation: "pile",
    cta: "看看囤了什么",
  },
  {
    eyebrow: "囤什么",
    lines: ["一个仓", "装下所有凭证"],
    desc: "session、oauth、key.tavily 统一进 artifact store，按插件和类型分好，不再散落在各处的文件里。",
    formation: "fan",
    cta: "怎么开始",
  },
  {
    eyebrow: "怎么开始",
    lines: ["先跑一趟注册", "剩下的交给它"],
    desc: "从「注册」发起一次流水线，产物会自动落进囤货区，号池和主从跟着一起动起来。",
    formation: "focus",
    cta: "开始使用",
  },
];

export function Onboarding() {
  const [open, setOpen] = useState(false);
  const [step, setStep] = useState(0);
  const [leaving, setLeaving] = useState(false);
  const [settled, setSettled] = useState(false);
  const [started, setStarted] = useState(false);

  const rootRef = useRef<HTMLDivElement>(null);
  const displaceRef = useRef<SVGFEDisplacementMapElement>(null);

  // 只在客户端判定首帧，避免静态导出的 SSR 产物与水合结果不一致
  useEffect(() => {
    if (!hasSeenOnboarding()) setOpen(true);
  }, []);

  const close = useCallback(() => {
    setLeaving(true);
    markOnboardingSeen();
    const reduced = prefersReducedMotion();
    window.setTimeout(() => setOpen(false), reduced ? 160 : 380);
  }, []);

  const next = useCallback(() => {
    setStep((s) => {
      if (s >= STEPS.length - 1) {
        close();
        return s;
      }
      return s + 1;
    });
  }, [close]);

  const prev = useCallback(() => setStep((s) => Math.max(0, s - 1)), []);

  // 纸面折平之后立方体才归位；再之后卸掉 3D 上下文，让正文回到像素级清晰
  useEffect(() => {
    if (!open) return;
    const reduced = prefersReducedMotion();
    const fly = window.setTimeout(() => setStarted(true), reduced ? 0 : 420);
    const flat = window.setTimeout(() => setSettled(true), reduced ? 220 : 1150);
    const focus = window.setTimeout(() => {
      rootRef.current?.querySelector<HTMLElement>("[data-sq-primary] button")?.focus();
    }, reduced ? 240 : 1150);
    return () => {
      window.clearTimeout(fly);
      window.clearTimeout(flat);
      window.clearTimeout(focus);
    };
  }, [open]);

  // 揉搓：位移强度从最大平滑收到 0，纸面由褶皱恢复成平面
  useEffect(() => {
    if (!open) return;
    const node = displaceRef.current;
    if (!node) return;
    if (prefersReducedMotion()) {
      node.setAttribute("scale", "0");
      return;
    }
    const duration = 1000;
    let start = 0;
    let raf = 0;
    const tick = (now: number) => {
      if (!start) start = now;
      const t = Math.min(1, (now - start) / duration);
      const eased = 1 - Math.pow(1 - t, 4);
      node.setAttribute("scale", String((1 - eased) * 64));
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  // 指针视差：直接写 CSS 变量，不触发 React 重渲染
  useEffect(() => {
    if (!open || prefersReducedMotion()) return;
    const root = rootRef.current;
    if (!root) return;
    const onMove = (e: PointerEvent) => {
      const x = (e.clientX / window.innerWidth) * 2 - 1;
      const y = (e.clientY / window.innerHeight) * 2 - 1;
      root.style.setProperty("--px", x.toFixed(3));
      root.style.setProperty("--py", y.toFixed(3));
    };
    const onLeave = () => {
      root.style.setProperty("--px", "0");
      root.style.setProperty("--py", "0");
    };
    window.addEventListener("pointermove", onMove, { passive: true });
    window.addEventListener("pointerleave", onLeave);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerleave", onLeave);
    };
  }, [open]);

  // Esc 关闭、左右键翻页、Tab 圈在弹层内
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close();
        return;
      }
      if (e.key === "ArrowRight") {
        e.preventDefault();
        next();
        return;
      }
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        prev();
        return;
      }
      if (e.key !== "Tab") return;
      const root = rootRef.current;
      if (!root) return;
      const focusable = root.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const active = document.activeElement;
      if (e.shiftKey && (active === first || !root.contains(active))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, close, next, prev]);

  if (!open) return null;

  const current = STEPS[step];
  const poses = FORMATIONS[started ? current.formation : "enter"];
  // 首屏文案要等纸面折平再浮现，后续切换立即出现
  const charBase = step === 0 ? 560 : 0;

  // 必须挂到 body：Sidebar.Provider 外层的 isolation: isolate 会成为 backdrop root，
  // 留在里面 backdrop-filter 采样不到页面内容，背景不会被模糊。
  return createPortal(
    <div
      ref={rootRef}
      className="sq-ob"
      data-leaving={leaving}
      role="dialog"
      aria-modal="true"
      aria-labelledby="sq-ob-title"
      aria-describedby="sq-ob-desc"
    >
      <svg className="sq-ob__defs" aria-hidden focusable="false">
        <filter id="sq-crumple" x="-12%" y="-12%" width="124%" height="124%">
          <feTurbulence
            type="fractalNoise"
            baseFrequency="0.011 0.026"
            numOctaves={3}
            seed={7}
            result="noise"
          />
          <feDisplacementMap
            ref={displaceRef}
            in="SourceGraphic"
            in2="noise"
            scale={64}
            xChannelSelector="R"
            yChannelSelector="G"
          />
        </filter>
      </svg>

      <div className="sq-ob__backdrop" onClick={close} />

      <div className="sq-ob__space">
        <div className="sq-ob__stage" data-settled={settled}>
          <div className="sq-ob__fold" aria-hidden>
            {Array.from({ length: STRIPS }, (_, i) => (
              <div
                key={i}
                className="sq-ob__strip"
                style={
                  {
                    "--i": i,
                    "--n": STRIPS,
                    "--d": STRIPS - 1 - i,
                    "--a": `${(i % 2 === 0 ? -1 : 1) * (30 + ((i * 41) % 34))}deg`,
                    "--z": `${((i * 29) % 40) - 20}px`,
                    "--origin": i % 2 === 0 ? "top" : "bottom",
                  } as React.CSSProperties
                }
              />
            ))}
          </div>

          <div className="sq-ob__crumple" aria-hidden />

          <div className="sq-ob__content">
            <div className="sq-ob__skip">
              <Button variant="ghost" size="sm" onClick={close}>
                跳过
              </Button>
            </div>

            <div className="sq-ob__sweep" aria-hidden />


            <div className="sq-ob__viewport" aria-hidden>
              <div className="sq-ob__aura" />
              <div className="sq-ob__halo" />
              <div className="sq-ob__orbit">
                <div className="sq-ob__scene">
                  <div className="sq-ob__grid" />
                  <div className="sq-ob__floor" />

                  <div className="sq-ob__mirror">
                    {poses.map((p, i) => (
                      <div
                        key={`mirror-${i}`}
                        className="sq-ob__cube"
                        data-brand={i === poses.length - 1}
                        style={
                          {
                            "--i": i,
                            "--x": p.x,
                            "--y": p.y,
                            "--z": `${p.z}px`,
                            "--rx": `${p.rx}deg`,
                            "--ry": `${p.ry}deg`,
                            "--k": p.k,
                            "--o": p.o,
                            "--s": 1,
                          } as React.CSSProperties
                        }
                      >
                        <CubeFaces logo={SERVICES[i]} mark={i === poses.length - 1} />
                      </div>
                    ))}
                  </div>

                  {poses.map((p, i) => (
                    <div
                      key={`shadow-${i}`}
                      className="sq-ob__shadow"
                      style={
                        {
                          "--i": i,
                          "--x": p.x,
                          "--z": `${p.z}px`,
                          "--k": p.k,
                          "--o": p.o,
                        } as React.CSSProperties
                      }
                    />
                  ))}

                  {poses.map((p, i) => (
                    <div
                      key={i}
                      className="sq-ob__cube"
                      data-brand={i === poses.length - 1}
                      style={
                        {
                          "--i": i,
                          "--x": p.x,
                          "--y": p.y,
                          "--z": `${p.z}px`,
                          "--rx": `${p.rx}deg`,
                          "--ry": `${p.ry}deg`,
                          "--k": p.k,
                          "--o": p.o,
                          "--s": 1,
                        } as React.CSSProperties
                      }
                    >
                      <CubeFaces logo={SERVICES[i]} mark={i === poses.length - 1} />
                    </div>
                  ))}

                  {/* 只在「囤」这一步跑进货流，后面两步讲的是别的事 */}
                  {started && step === 0
                    ? FLYERS.map((f, i) => (
                        <div
                          key={`flyer-${i}`}
                          className="sq-ob__flyer"
                          data-brand={f.brand ? "true" : "false"}
                          style={
                            {
                              "--fx": f.fx,
                              "--fz": f.fz,
                              "--fa": `${f.fa}deg`,
                              "--fb": `${f.fb}deg`,
                              "--tx": f.tx,
                              "--ty": f.ty,
                              "--tz": `${f.tz}px`,
                              "--s": f.size,
                              "--dur": `${f.dur}s`,
                              "--fd": `${f.delay}s`,
                            } as React.CSSProperties
                          }
                        >
                          <CubeFaces logo={SERVICES[i % SERVICES.length]} mark={f.brand} />
                        </div>
                      ))
                    : null}
                </div>
              </div>
              <div className="sq-ob__fade" />
            </div>

            <div className="sq-ob__copy">
              {current.brand ? (
                <span className="sq-ob__brand">
                  <Image
                    src="/brand/squirrel-icon.png"
                    alt=""
                    width={32}
                    height={32}
                    priority
                    className="sq-ob__mark"
                  />
                  {current.eyebrow}
                </span>
              ) : (
                <span className="sq-ob__eyebrow">{current.eyebrow}</span>
              )}
              <h2 className="sq-ob__title" id="sq-ob-title" key={`t-${step}`}>
                {renderLines(current.lines, charBase)}
              </h2>
              <p className="sq-ob__desc" id="sq-ob-desc" key={`d-${step}`}>
                {current.desc}
              </p>
            </div>

            <div className="sq-ob__footer">
              <div className="sq-ob__dots">
                {STEPS.map((s, i) => (
                  <button
                    key={s.eyebrow}
                    type="button"
                    className="sq-ob__dot"
                    aria-current={i === step}
                    aria-label={`第 ${i + 1} 步：${s.eyebrow}`}
                    onClick={() => setStep(i)}
                  />
                ))}
              </div>
              <div className="flex items-center gap-2">
                {step > 0 ? (
                  <Button variant="secondary" size="sm" onClick={prev}>
                    上一步
                  </Button>
                ) : null}
                <span data-sq-primary>
                  <Button size="sm" onClick={next}>
                    {current.cta}
                  </Button>
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** 按字符切分标题，逐字上浮出场 */
function renderLines(lines: string[], base: number) {
  let ci = 0;
  return lines.map((line, li) => (
    <span key={li} className="sq-ob__line">
      {Array.from(line).map((ch, i) => (
        <span
          key={`${li}-${i}`}
          className="sq-ob__char"
          style={{ "--ci": ci++, "--base": `${base}ms` } as React.CSSProperties}
        >
          {ch}
        </span>
      ))}
    </span>
  ));
}
