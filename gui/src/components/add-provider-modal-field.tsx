import type { ReactNode } from "react";

/**
 * One labelled row of the Add Provider form.
 *
 * The row is a `label`, so clicking the caption focuses the control inside it.
 * `.add-provider-field` spaces the row in the form column; `.field-label` is
 * the caption style every Add Provider field shares.
 */
const FIELD_ROW_CLASS = "add-provider-field";
const FIELD_CAPTION_CLASS = "field-label";

export type AddProviderFieldProps = {
  label: string;
  children: ReactNode;
};

export function AddProviderField(props: AddProviderFieldProps) {
  const caption = <span className={FIELD_CAPTION_CLASS}>{props.label}</span>;
  return <label className={FIELD_ROW_CLASS}>{caption}{props.children}</label>;
}
