// SPDX-License-Identifier: AGPL-3.0-only
// Browser-only conformance host. Imports are substituted by amll_reference.py;
// the reference sources and its DOM/CSS runtime are never used by the Go app.
import { DomLyricPlayer } from "REFERENCE_DOM";
import { parseTTML } from "REFERENCE_TTML";

const config = "REFERENCE_CONFIG";
const stage = document.getElementById("stage")!;
Object.assign(stage.style, {
  width: `${config.width}px`,
  height: `${config.height}px`,
  fontFamily: config.font,
});
stage.style.setProperty("--amll-lp-font-size", `${config.fontSize}px`);
const input = parseTTML(await (await fetch("/lyrics.ttml")).text());
const player = new DomLyricPlayer();
stage.append(player.getElement());
player.setEnableAutoSeekDetection(false);
player.setLyricLines(input.lines, config.position);
player.resume();
let position = config.position;
let advancing = false;
let previous = performance.now();
let frames = 0;
function frame(now: number) {
  const delta = Math.min(100, Math.max(0, now - previous));
  previous = now;
  if (advancing) position += delta;
  player.setCurrentTime(position);
  player.update(delta);
  frames++;
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

// This host uses the real browser animation timeline. A held media position
// keeps wall-clock float/emphasis running; it is not a deterministic replay.
(window as any).reference = {
  player,
  config,
  setTime(ms: number) {
    if (!Number.isFinite(ms) || ms < 0) throw new RangeError("invalid media time");
    position = ms;
    advancing = false;
    player.setCurrentTime(position, true);
  },
  play() { player.resume(); advancing = true; },
  hold() { advancing = false; },
  pause() { advancing = false; player.pause(); },
  resume() { player.resume(); },
  stats() {
    return {
      ...config,
      inputLines: input.lines.length,
      groups: player.currentLyricGroups.length,
      position,
      frames,
      hover: player.getElement().matches(":hover"),
      devicePixelRatio,
      viewport: player.size,
      animations: document.getAnimations().length,
      supportPlusLighter: player.supportPlusLighter,
      supportMaskImage: player.supportMaskImage,
    };
  },
  layout() {
    return player.currentLyricGroups.map((group, index) => {
      const element = group.getElement();
      const main = group.mainLine.getElement();
      const bg = group.bgLine?.getElement();
      function measure(el: HTMLElement | undefined) {
        if (!el?.isConnected) return null;
        const style = getComputedStyle(el);
        return {
          rect: el.getBoundingClientRect().toJSON(),
          client: [el.clientWidth, el.clientHeight],
          transform: style.transform,
          opacity: style.opacity,
          filter: style.filter,
          font: style.font,
        };
      }
      return { index, group: measure(element), main: measure(main), bg: measure(bg) };
    }).filter(row => row.group);
  },
};
document.documentElement.dataset.ready = "true";
