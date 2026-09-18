/** Public Models catalogue adapter for the provider hint surface. */
import { createElement } from "react";
import { useT } from "../i18n/shared";
import {
  ProviderHintSurface,
  type ProviderHintSurfaceInput,
} from "./models-provider-hint-surface";

type EmptyProviderHintProps = Omit<ProviderHintSurfaceInput, "t">;

export function EmptyProviderHint(props: EmptyProviderHintProps) {
  return createElement(ProviderHintSurface, { ...props, t: useT() });
}
