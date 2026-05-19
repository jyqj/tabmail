export function TabMailLogo({ size = 22 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <rect x="2" y="6" width="20" height="14" rx="3" className="fill-primary" />
      <rect x="2" y="6" width="20" height="3" rx="1.5" className="fill-primary" opacity="0.35" />
      <path
        d="m4 10 8 5 8-5"
        stroke="white"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
