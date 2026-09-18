import type { SVGProps } from "react";
import { routingProfileIconPath } from "./routing-profile-icon-data";

export function RoutingProfileIcon({
  id,
  ...props
}: { id?: string | null } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={14}
      height={14}
      aria-hidden="true"
      focusable="false"
      {...props}
    >
      <path fill="currentColor" d={routingProfileIconPath(id)} />
    </svg>
  );
}
