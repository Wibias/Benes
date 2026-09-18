/**
 * The Add Provider connection form.
 *
 * Section order lives here and nowhere else: identity, then the
 * adapter/endpoint block a non-reserved row needs, then the auth panel for the
 * selected method. A reserved Codex row keeps only the identity and auth
 * panels. The fields themselves are owned by ./add-provider-form-sections.tsx.
 */
import type { CatalogPreset } from "./provider-catalog/provider-presets";
import {
  AddProviderAdapterField,
  AddProviderAuthFields,
  AddProviderDefaultModelField,
  AddProviderEndpointFields,
  AddProviderFormActions,
  AddProviderIdentityFields,
  AddProviderNetworkFields,
  AddProviderSetupGuide,
  type AddProviderChoiceHandler,
  type AddProviderDraft,
  type AddProviderDraftWriter,
  type AddProviderVoidHandler,
} from "./add-provider-form-sections";

/** Everything the form reads about the row it is editing. */
export type AddProviderFormState = {
  draft: AddProviderDraft;
  endpointChoice: string;
  error: string;
  saving: boolean;
  dup: boolean;
  isCustom: boolean;
  isLocal: boolean;
  isReservedForward: boolean;
};

/** Everything the form can ask the wizard to do. */
export type AddProviderFormHandlers = {
  write: AddProviderDraftWriter;
  chooseEndpoint: AddProviderChoiceHandler;
  submit: AddProviderVoidHandler;
  useOauthLogin: AddProviderVoidHandler;
  back: AddProviderVoidHandler;
};

export function AddProviderFormPane({
  preset,
  presetDescription,
  state,
  handlers,
  embedded = false,
}: {
  preset: CatalogPreset;
  presetDescription: (candidate: CatalogPreset) => string | undefined;
  state: AddProviderFormState;
  handlers: AddProviderFormHandlers;
  embedded?: boolean;
}) {
  const { draft, endpointChoice, error, saving, dup, isCustom, isLocal, isReservedForward } = state;
  const { write, chooseEndpoint, submit, useOauthLogin, back } = handlers;
  const showsAdapterAndEndpoint = !isReservedForward;

  return (
    <div className="add-provider-form">
      {!embedded && (
        <AddProviderSetupGuide
          preset={preset}
          draft={draft}
          isReservedForward={isReservedForward}
          isCustom={isCustom}
          isLocal={isLocal}
        />
      )}
      <AddProviderIdentityFields draft={draft} dup={dup} isReservedForward={isReservedForward} write={write} />
      {showsAdapterAndEndpoint && (
        <>
          <AddProviderAdapterField draft={draft} write={write} />
          <AddProviderEndpointFields
            preset={preset}
            draft={draft}
            endpointChoice={endpointChoice}
            write={write}
            chooseEndpoint={chooseEndpoint}
          />
          <AddProviderNetworkFields draft={draft} write={write} />
        </>
      )}
      <AddProviderAuthFields
        preset={preset}
        draft={draft}
        presetDescription={presetDescription}
        write={write}
      />
      {showsAdapterAndEndpoint && <AddProviderDefaultModelField draft={draft} write={write} />}
      {error && <div className="add-provider-form-note"><div className="text-control" role="alert" style={{ color: "var(--red)" }}>{error}</div></div>}
      {!embedded && (
        <AddProviderFormActions
          saving={saving}
          preset={preset}
          submit={submit}
          useOauthLogin={useOauthLogin}
          back={back}
        />
      )}
    </div>
  );
}
