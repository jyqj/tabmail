export function TabMailLogo({ size = 32 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <defs>
        <linearGradient id="tm-bg" x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#0d9488" />
          <stop offset="100%" stopColor="#06b6d4" />
        </linearGradient>
        <linearGradient id="tm-flap" x1="8" y1="9" x2="24" y2="9" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#ffffff" stopOpacity="0.95" />
          <stop offset="100%" stopColor="#ffffff" stopOpacity="0.75" />
        </linearGradient>
      </defs>
      {/* Rounded square background */}
      <rect width="32" height="32" rx="8" fill="url(#tm-bg)" />
      {/* Envelope body */}
      <rect x="7" y="10" width="18" height="13" rx="2" fill="white" fillOpacity="0.2" />
      {/* Envelope V-flap */}
      <path
        d="M7.5 10.5L16 17.5L24.5 10.5"
        stroke="white"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
      {/* Envelope bottom edges */}
      <path
        d="M7.5 22.5L13 17.5M24.5 22.5L19 17.5"
        stroke="white"
        strokeWidth="1.2"
        strokeLinecap="round"
        strokeOpacity="0.4"
      />
      {/* Tab accent - small folded tab top-right */}
      <path
        d="M21 6L25.5 10.5L21 10.5Z"
        fill="url(#tm-flap)"
      />
      {/* Tiny send arrow */}
      <circle cx="22.5" cy="7.5" r="1.2" fill="white" fillOpacity="0.9" />
    </svg>
  );
}
