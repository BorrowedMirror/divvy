import glyphUrl from "../assets/rkl-glyph.svg";

/** The Chaos Claw glyph (assets/brand). Decorative: the adjacent heading names the screen. */
export function BrandMark({ size = 24 }: { size?: number }) {
  return <img src={glyphUrl} width={size} height={size} alt="" aria-hidden="true" className="shrink-0" />;
}
