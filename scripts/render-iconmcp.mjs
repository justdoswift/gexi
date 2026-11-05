#!/usr/bin/env node
// Renders the IconMCP glyphs listed in icons/iconmcp.json to the same
// 192px-canvas PNGs scripts/render-icons.swift produced for SF Symbols,
// so icon.go can keep treating them as alpha masks.
//
// Each entry carries its own box and stroke: IconMCP draws on a 16-unit
// grid, and box/stroke were tuned per icon so its ink covers the same area,
// and reads at the same weight, as the SF Symbol it replaces. Re-run this
// only after editing those numbers by hand — nothing here infers them.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const CANVAS = 192;
const CHROME =
  process.env.CHROME ||
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";

const spec = JSON.parse(
  fs.readFileSync(new URL("../icons/iconmcp.json", import.meta.url), "utf8")
);
const grid = spec.grid;
const outDir = new URL("../icons/", import.meta.url);

const esc = (n) => String(n).replace(/"/g, "&quot;");

function el(e, stroke) {
  const a = [];
  const push = (k, v) => a.push(`${k}="${esc(v)}"`);
  if (e.t === "rect") {
    push("x", e.x); push("y", e.y); push("width", e.w); push("height", e.h);
    push("rx", e.rs ?? 1);
    return `<rect ${a.join(" ")} fill="none"/>`;
  }
  if (e.t === "line") {
    push("x1", e.x1); push("y1", e.y1); push("x2", e.x2); push("y2", e.y2);
    return `<line ${a.join(" ")} fill="none"/>`;
  }
  if (e.t === "circle") {
    push("cx", e.cx); push("cy", e.cy); push("r", e.r);
    return `<circle ${a.join(" ")} fill="none"/>`;
  }
  if (e.t === "ellipse") {
    push("cx", e.cx); push("cy", e.cy); push("rx", e.rx); push("ry", e.ry);
    return `<ellipse ${a.join(" ")} fill="none"/>`;
  }
  if (e.t === "dot") {
    // A dot is a filled disc sized against the stroke, as IconMCP draws it.
    return `<circle cx="${esc(e.cx)}" cy="${esc(e.cy)}" r="${stroke * 0.66}" fill="currentColor" stroke="none"/>`;
  }
  if (e.t === "poly") {
    const tag = e.closed ? "polygon" : "polyline";
    const pts = e.pts.map((p) => `${p[0]},${p[1]}`).join(" ");
    return `<${tag} points="${esc(pts)}" fill="none"/>`;
  }
  if (e.t === "path") return `<path d="${esc(e.d)}" fill="none"/>`;
  throw new Error(`unknown element ${e.t}`);
}

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "iconmcp-"));
const written = [];

for (const [file, icon] of Object.entries(spec.icons)) {
  const box = icon.box;
  const stroke = icon.stroke;
  const body = icon.els.map((e) => el(e, stroke)).join("");
  const rotated = icon.rotate
    ? `<g transform="rotate(${icon.rotate} 8 8)">${body}</g>`
    : body;
  const shift = icon.dx || icon.dy
    ? `transform:translate(${icon.dx}px,${icon.dy}px);`
    : "";
  const html = `<!doctype html><meta charset="utf-8">
<style>html,body{margin:0;background:transparent}
.box{width:${CANVAS}px;height:${CANVAS}px;display:flex;align-items:center;justify-content:center}</style>
<div class="box"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${grid} ${grid}"
 width="${box}" height="${box}" fill="none" stroke="#000" stroke-width="${stroke}"
 stroke-linecap="round" stroke-linejoin="round" color="#000" style="${shift}">${rotated}</svg></div>`;

  const page = path.join(tmp, file + ".html");
  const png = path.join(tmp, file + ".png");
  fs.writeFileSync(page, html);

  execFileSync(CHROME, [
    "--headless=new",
    "--disable-gpu",
    "--hide-scrollbars",
    "--force-device-scale-factor=1",
    "--default-background-color=00000000",
    `--window-size=${CANVAS},${CANVAS}`,
    `--screenshot=${png}`,
    "file://" + page,
  ], { stdio: "ignore" });

  fs.copyFileSync(png, new URL(file + ".png", outDir));
  written.push(file);
}

fs.rmSync(tmp, { recursive: true, force: true });
console.log(`wrote ${written.length} icons: ${written.join(", ")}`);
