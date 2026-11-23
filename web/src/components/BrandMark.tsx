import markUrl from "@assets/divvy-mark.png";

/** The Divine Orb mark (assets/brand). Decorative: the adjacent heading names the screen. */
export function BrandMark({ size = 24 }: { size?: number }) {
  return <img src={markUrl} width={size} height={size} alt="" aria-hidden="true" className="shrink-0" />;
}
