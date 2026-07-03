// Inline SVG icons carried over from the design.
const P = (props: { d: string }) => <path d={props.d} />;

const svg = (size: number, stroke: string, strokeWidth: number, children: React.ReactNode, style?: React.CSSProperties) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke={stroke} strokeWidth={strokeWidth} style={style}>
    {children}
  </svg>
);

export const IconBranch = () => svg(13, 'var(--text-2)', 2, <>
  <circle cx="6" cy="6" r="2.4" /><circle cx="6" cy="18" r="2.4" /><circle cx="18" cy="9" r="2.4" />
  <P d="M6 8.4v7.2M8.3 7.3l7.5 1.4M18 11.4c0 4-6 2.6-6 6.6" /></>);

export const IconSearch = () => svg(14, 'currentColor', 2, <><circle cx="11" cy="11" r="7" /><P d="M20 20l-3.5-3.5" /></>);

export const IconPR = () => svg(13, 'var(--prod)', 2, <>
  <circle cx="6" cy="6" r="2.4" /><circle cx="6" cy="18" r="2.4" /><circle cx="18" cy="18" r="2.4" />
  <P d="M6 8.4v7.2M18 15.6c0-5-12-2-12-9.6" /></>);

export const IconDash = () => svg(18, 'currentColor', 1.8, <>
  <rect x="3.5" y="3.5" width="7.5" height="7.5" rx="1.6" /><rect x="13" y="3.5" width="7.5" height="4.5" rx="1.6" />
  <rect x="13" y="11" width="7.5" height="9.5" rx="1.6" /><rect x="3.5" y="14" width="7.5" height="6.5" rx="1.6" /></>);

export const IconFolder = () => svg(19, 'currentColor', 1.8,
  <P d="M4 5.5A1.5 1.5 0 015.5 4h4l2 2.5h7A1.5 1.5 0 0120 8v9.5a1.5 1.5 0 01-1.5 1.5h-13A1.5 1.5 0 014 17.5z" />);

export const IconChanges = () => svg(19, 'currentColor', 1.8, <P d="M4 12h6M14 12h6M8 8l-4 4 4 4M16 16l4-4-4-4" />);

export const IconTrace = ({ size = 19, width = 1.8 }: { size?: number; width?: number }) => svg(size, 'currentColor', width, <>
  <circle cx="6" cy="6" r="2.3" /><circle cx="18" cy="6" r="2.3" /><circle cx="12" cy="18" r="2.3" />
  <P d="M7.5 7.4l3.3 8.4M16.5 7.4l-3.3 8.4M8 6h8" /></>);

export const IconMatrix = () => svg(19, 'currentColor', 1.8, <>
  <rect x="3" y="4" width="18" height="16" rx="1.6" /><P d="M3 9h18M3 14h18M9 4v16M15 4v16" /></>);

export const IconModel = () => svg(19, 'currentColor', 1.8, <>
  <P d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" /><P d="M12 3v18M4 7.5l8 4.5 8-4.5" /></>);

export const IconSpark = ({ size = 19, stroke = 'currentColor', width = 1.8 }: { size?: number; stroke?: string; width?: number }) =>
  svg(size, stroke, width, <P d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z" />);

export const IconGear = () => svg(18, 'currentColor', 1.8, <>
  <circle cx="12" cy="12" r="3" /><P d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2" /></>);

export const IconChevR = ({ size = 11 }: { size?: number }) => svg(size, 'currentColor', 2.4, <P d="M8 6l6 6-6 6" />);
export const IconChevD = () => svg(11, 'currentColor', 2.4, <P d="M6 9l6 6 6-6" />);
export const IconPlus = () => svg(14, 'currentColor', 2, <P d="M12 5v14M5 12h14" />);
export const IconSync = () => svg(13, 'currentColor', 2, <P d="M20 11a8 8 0 10-2.3 5.7M20 5v6h-6" />);
export const IconClose = () => svg(12, 'currentColor', 2, <P d="M6 6l12 12M18 6L6 18" />, { opacity: 0.6 });
export const IconShare = () => svg(12, 'currentColor', 2, <P d="M4 12v7a1 1 0 001 1h14a1 1 0 001-1v-7M16 6l-4-4-4 4M12 2v14" />);
export const IconUp = () => svg(12, 'currentColor', 2, <P d="M12 19V5M6 11l6-6 6 6" />);
export const IconDown = () => svg(12, 'currentColor', 2, <P d="M12 5v14M6 13l6 6 6-6" />);
export const IconSend = () => svg(14, 'currentColor', 2.2, <P d="M12 19V5M6 11l6-6 6 6" />);

export const IconLock = ({ size = 11 }: { size?: number }) => svg(size, 'currentColor', 1.9, <>
  <rect x="5" y="10.5" width="14" height="9.5" rx="2" /><P d="M8.2 10.5V7.2a3.8 3.8 0 017.6 0v3.3" /></>);

export const IconMenu = () => svg(15, 'currentColor', 2, <P d="M4 6.5h16M4 12h16M4 17.5h16" />);

export const IconDiagram = ({ size = 13 }: { size?: number }) => svg(size, 'currentColor', 1.8, <>
  <rect x="3.5" y="3.5" width="8" height="6" rx="1.4" /><rect x="12.5" y="14.5" width="8" height="6" rx="1.4" />
  <P d="M7.5 9.5v4.5a2 2 0 002 2h3" /></>);

export const IconPen = ({ size = 12 }: { size?: number }) => svg(size, 'currentColor', 1.8, <>
  <P d="M14.8 4.8l4.4 4.4L8 20.4H3.6V16z" /><P d="M12.6 7l4.4 4.4" /></>);

export const IconImage = ({ size = 13 }: { size?: number }) => svg(size, 'currentColor', 1.8, <>
  <rect x="3.5" y="4.5" width="17" height="15" rx="1.8" /><circle cx="9" cy="10" r="1.7" />
  <P d="M4.5 17.5l4.7-4.7 3.6 3.6 2.7-2.7 4.5 4.5" /></>);

export const IconLink = ({ size = 12 }: { size?: number }) => svg(size, 'currentColor', 1.8, <>
  <P d="M10 14l4-4" /><P d="M8.5 11.5L6 14a3.5 3.5 0 105 5l2.5-2.5" /><P d="M15.5 12.5L18 10a3.5 3.5 0 10-5-5l-2.5 2.5" /></>);

export const IconUserPlus = ({ size = 12 }: { size?: number }) => svg(size, 'currentColor', 1.8, <>
  <circle cx="9" cy="8" r="3.4" /><P d="M3.5 20c.4-3.2 2.7-5 5.5-5s5.1 1.8 5.5 5" /><P d="M18 7.5v6M15 10.5h6" /></>);

export const IconArrowLR = () => (
  <svg width="20" height="12" viewBox="0 0 20 12" fill="none" stroke="var(--text-3)" strokeWidth="1.5">
    <path d="M1 6h16M13 2l4 4-4 4" />
  </svg>
);
