import type { SVGProps } from "react";

const base: SVGProps<SVGSVGElement> = {
  width: 14,
  height: 14,
  viewBox: "0 0 16 16",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.6,
  strokeLinecap: "round",
  strokeLinejoin: "round",
};

export const GearIcon = () => (
  <svg {...base} aria-hidden="true">
    <circle cx="8" cy="8" r="2.2" />
    <path d="M8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4" />
  </svg>
);

export const PinIcon = ({ filled }: { filled?: boolean }) => (
  <svg {...base} aria-hidden="true">
    <path d="M6 2h4l-.5 4 2.5 2.5H4L6.5 6 6 2Z" fill={filled ? "currentColor" : "none"} />
    <path d="M8 8.5V14" />
  </svg>
);

export const CloseIcon = () => (
  <svg {...base} aria-hidden="true">
    <path d="M4 4l8 8M12 4l-8 8" />
  </svg>
);

export const ExternalIcon = () => (
  <svg {...base} aria-hidden="true">
    <path d="M9 3h4v4M13 3 7.5 8.5M11 9.5V13H3V5h3.5" />
  </svg>
);
