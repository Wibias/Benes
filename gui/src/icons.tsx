/**
 * The dashboard's icon vocabulary.
 *
 * Every glyph is drawn on the same 24-unit grid with the same stroke contract, so the only
 * thing that differs between two icons is what is inside the `<svg>`. One factory applies that
 * contract instead of the same eight attributes being repeated per icon, and each glyph is
 * built once at module load rather than re-allocated on every render.
 *
 * The marks follow the Lucide drawing conventions this dashboard already shipped; icon path
 * geometry is the rendered contract the stylesheet and components were sized against.
 */
import type { ReactNode, SVGProps } from "react";

/** Props an icon accepts. Anything extra is forwarded to the `<svg>`, so a caller's own
 *  `fill`, `stroke`, `className`, or `aria-hidden` still wins. */
export type IconProps = SVGProps<SVGSVGElement>;

/** The stroke contract every icon shares. */
const STROKE_ICON = Object.freeze({
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
});

/**
 * Build one icon from its drawing.
 *
 * `displayName` is set because a factory erases it: without it every icon would report the same
 * name in devtools and the React profiler, which would make the vocabulary harder to read than
 * the per-icon declarations it replaces.
 */
function strokeIcon(displayName: string, glyph: ReactNode) {
  return Object.assign(
    (props: IconProps) => <svg {...STROKE_ICON} {...props}>{glyph}</svg>,
    { displayName },
  );
}

export const IconGrid = strokeIcon("IconGrid",
  <><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></>,
);

export const IconServer = strokeIcon("IconServer",
  <><rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/></>,
);

export const IconBoxes = strokeIcon("IconBoxes",
  <><path d="M12 2 4 6v6l8 4 8-4V6l-8-4Z"/><path d="m4 6 8 4 8-4M12 10v8"/></>,
);

export const IconBot = strokeIcon("IconBot",
  <><rect x="4" y="8" width="16" height="11" rx="3"/><path d="M12 8V4M8 2h8"/><circle cx="9" cy="13" r="1"/><circle cx="15" cy="13" r="1"/></>,
);

export const IconList = strokeIcon("IconList",
  <><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></>,
);

export const IconMenu = strokeIcon("IconMenu",
  <><path d="M4 6h16M4 12h16M4 18h16"/></>,
);

export const IconTerminal = strokeIcon("IconTerminal",
  <><path d="m4 17 6-5-6-5"/><path d="M12 19h8"/></>,
);

export const IconActivity = strokeIcon("IconActivity",
  <><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></>,
);

export const IconHardDrive = strokeIcon("IconHardDrive",
  <><path d="M22 12H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11Z"/><path d="M6 16h.01M10 16h.01"/></>,
);

export const IconCheck = strokeIcon("IconCheck",
  <><path d="m20 6-11 11-5-5"/></>,
);

export const IconCircle = strokeIcon("IconCircle",
  <><circle cx="12" cy="12" r="8"/></>,
);

export const IconX = strokeIcon("IconX",
  <><path d="M18 6 6 18M6 6l12 12"/></>,
);

export const IconPlus = strokeIcon("IconPlus",
  <><path d="M12 5v14M5 12h14"/></>,
);

export const IconRefresh = strokeIcon("IconRefresh",
  <><path d="M21 12a9 9 0 0 1-9 9 9.8 9.8 0 0 1-6.7-2.7L3 16M3 21v-5h5M3 12a9 9 0 0 1 9-9 9.8 9.8 0 0 1 6.7 2.7L21 8M21 3v5h-5"/></>,
);

export const IconPause = strokeIcon("IconPause",
  <><path d="M8 5v14M16 5v14"/></>,
);

export const IconPlay = strokeIcon("IconPlay",
  <><path d="m7 4 13 8-13 8Z"/></>,
);

export const IconTrash = strokeIcon("IconTrash",
  <><path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/></>,
);

export const IconAlert = strokeIcon("IconAlert",
  <><path d="M10.3 3.7 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.7a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></>,
);

export const IconAlertCircle = strokeIcon("IconAlertCircle",
  <><circle cx="12" cy="12" r="10"/><path d="M12 8v5M12 16h.01"/></>,
);

export const IconInfo = strokeIcon("IconInfo",
  <><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></>,
);

export const IconHelp = strokeIcon("IconHelp",
  <><circle cx="12" cy="12" r="10"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 2-3 4"/><path d="M12 17h.01"/></>,
);

export const IconSearch = strokeIcon("IconSearch",
  <><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></>,
);

export const IconArrowUp = strokeIcon("IconArrowUp",
  <><path d="M12 19V5M5 12l7-7 7 7"/></>,
);

export const IconArrowDown = strokeIcon("IconArrowDown",
  <><path d="M12 5v14M19 12l-7 7-7-7"/></>,
);

export const IconArrowLeft = strokeIcon("IconArrowLeft",
  <><path d="M19 12H5M12 19l-7-7 7-7"/></>,
);

export const IconDownload = strokeIcon("IconDownload",
  <><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/></>,
);

export const IconChevron = strokeIcon("IconChevron",
  <><path d="m9 18 6-6-6-6"/></>,
);

export const IconGithub = strokeIcon("IconGithub",
  <><path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.9a3.4 3.4 0 0 0-.9-2.6c3-.3 6.2-1.5 6.2-6.7A5.2 5.2 0 0 0 20 4.8 4.9 4.9 0 0 0 19.9 1S18.7.6 16 2.5a13.4 13.4 0 0 0-7 0C6.3.6 5.1 1 5.1 1A4.9 4.9 0 0 0 5 4.8a5.2 5.2 0 0 0-1.4 3.7c0 5.1 3.1 6.4 6.1 6.7a3.4 3.4 0 0 0-.9 2.5V22"/></>,
);

export const IconPower = strokeIcon("IconPower",
  <><path d="M18.4 5.6a9 9 0 1 1-12.8 0"/><path d="M12 2v10"/></>,
);

export const IconExternal = strokeIcon("IconExternal",
  <><path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/></>,
);

export const IconKey = strokeIcon("IconKey",
  <><circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.7 12.3 9.6-9.6M16 7l3 3M14 9l2 2"/></>,
);

export const IconEye = strokeIcon("IconEye",
  <><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></>,
);

export const IconEyeOff = strokeIcon("IconEyeOff",
  <><path d="M3 3l18 18M10.6 10.6A3 3 0 0 0 13.4 13.4M9.9 5.1A10.9 10.9 0 0 1 12 5c6 0 10 7 10 7a18 18 0 0 1-2.2 3.1M6.1 6.1A17.7 17.7 0 0 0 2 12s4 7 10 7a10.4 10.4 0 0 0 4.2-.9"/></>,
);

export const IconLock = strokeIcon("IconLock",
  <><rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/></>,
);

export const IconShield = strokeIcon("IconShield",
  <><path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6Z"/></>,
);

export const IconCpu = strokeIcon("IconCpu",
  <><rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/><rect x="9" y="9" width="6" height="6" rx="1"/></>,
);

export const IconGauge = strokeIcon("IconGauge",
  <><path d="M5.4 19a9 9 0 1 1 13.2 0"/><path d="M12 13V9"/><path d="m16 10 2-3"/></>,
);

export const IconTicket = strokeIcon("IconTicket",
  <><path d="M2 9a3 3 0 0 1 0 6v2a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-2a3 3 0 0 1 0-6V7a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2Z"/><path d="M13 5v2"/><path d="M13 17v2"/><path d="M13 11v2"/></>,
);

export const IconLink = strokeIcon("IconLink",
  <><path d="M9 17H7A5 5 0 0 1 7 7h2M15 7h2a5 5 0 0 1 0 10h-2M8 12h8"/></>,
);

export const IconSun = strokeIcon("IconSun",
  <><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></>,
);

export const IconMoon = strokeIcon("IconMoon",
  <><path d="M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8Z"/></>,
);

export const IconMonitor = strokeIcon("IconMonitor",
  <><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></>,
);

export const IconGlobe = strokeIcon("IconGlobe",
  <><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></>,
);

export const IconShuffle = strokeIcon("IconShuffle",
  <><path d="m18 14 4 4-4 4" />
    <path d="m18 2 4 4-4 4" />
    <path d="M2 18h1.973a4 4 0 0 0 3.3-1.7l5.454-8.6a4 4 0 0 1 3.3-1.7H22" />
    <path d="M2 6h1.972a4 4 0 0 1 3.6 2.2" />
    <path d="M22 18h-6.041a4 4 0 0 1-3.3-1.8l-.359-.45" /></>,
);

export const IconGrip = strokeIcon("IconGrip",
  <><circle cx="9" cy="6" r="1" fill="currentColor" stroke="none" />
    <circle cx="15" cy="6" r="1" fill="currentColor" stroke="none" />
    <circle cx="9" cy="12" r="1" fill="currentColor" stroke="none" />
    <circle cx="15" cy="12" r="1" fill="currentColor" stroke="none" />
    <circle cx="9" cy="18" r="1" fill="currentColor" stroke="none" />
    <circle cx="15" cy="18" r="1" fill="currentColor" stroke="none" /></>,
);

export const IconStar = strokeIcon("IconStar",
  <><path d="m12 2 3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></>,
);

export const IconFilter = strokeIcon("IconFilter",
  <><path d="M4 5h16l-6 7v5l-4 2v-7L4 5z"/></>,
);

export const IconCopy = strokeIcon("IconCopy",
  <><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></>,
);

export const IconPencil = strokeIcon("IconPencil",
  <><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/></>,
);

export const IconMore = strokeIcon("IconMore",
  <><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></>,
);

export const IconUndo = strokeIcon("IconUndo",
  <><path d="M3 7v6h6" />
    <path d="M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6.7 2.9L3 13" /></>,
);

export const IconFile = strokeIcon("IconFile",
  <><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6"/></>,
);

export const IconFolder = strokeIcon("IconFolder",
  <><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.9-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></>,
);

export const IconUpload = strokeIcon("IconUpload",
  <><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12"/></>,
);
