ci_scope_enabled() {
  local needle="$1"
  local scope="${BENES_CI_SCOPE:-all}"
  if [[ "$scope" == "all" || -z "$scope" ]]; then
    return 0
  fi
  local part
  IFS=',' read -r -a parts <<< "$scope"
  for part in "${parts[@]}"; do
    if [[ "$part" == "$needle" ]]; then
      return 0
    fi
  done
  return 1
}
