{{- define "mall.image" -}}
{{- $global := .root.Values.global.image -}}
{{- $repository := required "image repository is required" (default $global.repository .image.repository) -}}
{{- $tag := required "image tag is required" (default $global.tag .image.tag) -}}
{{- $registry := default $global.registry .image.registry -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" (trimSuffix "/" $registry) $repository $tag -}}
{{- else -}}
{{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end -}}

{{- define "mall.password" -}}
{{- $existing := lookup "v1" "Secret" .root.Release.Namespace (printf "%s-credentials" .root.Release.Name) -}}
{{- if .value -}}
{{- .value -}}
{{- else if and $existing (hasKey $existing.data .key) -}}
{{- index $existing.data .key | b64dec -}}
{{- else -}}
{{- randAlphaNum .length -}}
{{- end -}}
{{- end -}}
