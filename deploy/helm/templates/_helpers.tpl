{{/* 公共标签与命名 */}}
{{- define "quenching.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "quenching.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "quenching.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{ include "quenching.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "quenching.selectorLabels" -}}
app.kubernetes.io/name: {{ include "quenching.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* 全部平台服务（顺序即网关路由无关，部署顺序无强依赖） */}}
{{- define "quenching.serviceNames" -}}
{{- /* g7：ws-gateway 已并入 api-gateway（同进程 /ws 与 /api/v1/events） */ -}}
{{- list "api-gateway" "content-service" "user-service" "training-service" "review-service" "fault-engine" "sandbox-service" "ai-service" -}}
{{- end -}}

{{/* 服务是否启用 */}}
{{- define "quenching.svcEnabled" -}}
{{- $root := index . 0 -}}
{{- $name := index . 1 -}}
{{- $s := index $root.Values.services $name -}}
{{- $s.enabled -}}
{{- end -}}

{{/* 服务类型：python / go-env（纯环境变量网关）/ go-yaml（configmap 挂载） */}}
{{- define "quenching.svcKind" -}}
{{- if eq . "ai-service" -}}python
{{- else if eq . "api-gateway" -}}go-env
{{- else -}}go-yaml
{{- end -}}
{{- end -}}

{{/* 镜像地址 */}}
{{- define "quenching.image" -}}
{{- $root := index . 0 -}}
{{- $name := index . 1 -}}
{{- $reg := $root.Values.global.imageRegistry -}}
{{- printf "%squenching/%s:%s" $reg $name $root.Values.global.imageTag -}}
{{- end -}}

{{/* Service DNS（同 namespace） */}}
{{- define "quenching.svcDNS" -}}{{- . -}}{{- end -}}

{{/* 日志持久化开关。nil 安全：--reuse-values 沿用旧值、缺 logging 段时默认 false。
     先经 JSON 往返把 chartutil.Values 转成普通 map，dig 才能正常工作。 */}}
{{- define "quenching.logEnabled" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "persistence" "enabled" false $v -}}
{{- end -}}

{{/* 日志落盘目录。nil 安全：缺 logging 段时用默认 /var/log/quenching。 */}}
{{- define "quenching.logDir" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "dir" "/var/log/quenching" $v -}}
{{- end -}}

{{/* 节点日志目录为空时，保留原 PVC 存储方式。 */}}
{{- define "quenching.logHostPath" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "persistence" "hostPath" "" $v -}}
{{- end -}}

{{/* 所有后端统一时区；Pod 名称用于多副本文件隔离。 */}}
{{- define "quenching.logEnv" -}}
- name: TZ
  value: {{ default "Asia/Shanghai" .Values.global.timezone | quote }}
{{- if eq (include "quenching.logEnabled" .) "true" }}
- name: LOG_DIR
  value: {{ include "quenching.logDir" . | quote }}
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
{{- end }}
{{- end -}}

{{/* 日志卷访问模式，默认 ReadWriteOnce。 */}}
{{- define "quenching.logAccessMode" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "persistence" "accessMode" "ReadWriteOnce" $v -}}
{{- end -}}

{{/* 日志卷容量，默认 10Gi。 */}}
{{- define "quenching.logSize" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "persistence" "size" "10Gi" $v -}}
{{- end -}}

{{/* 日志卷 StorageClass，默认空（用集群默认）。 */}}
{{- define "quenching.logStorageClass" -}}
{{- $v := .Values | mustToJson | fromJson -}}
{{- dig "logging" "persistence" "storageClass" "" $v -}}
{{- end -}}

{{/* PostgreSQL DSN */}}
{{- define "quenching.dbDsn" -}}
{{- $p := .Values.postgresql.external -}}
{{- printf "postgres://%s:%s@%s:%v/%s?sslmode=disable" $p.username $p.password $p.host $p.port $p.database -}}
{{- end -}}

{{/* RabbitMQ URL */}}
{{- define "quenching.amqpUrl" -}}
{{- $r := .Values.rabbitmq.external -}}
{{- printf "amqp://%s:%s@%s:%v/" $r.username $r.password $r.host $r.port -}}
{{- end -}}

{{/* platform-secret 校验和素材：字段须与 secret.yaml 中 platform-secret
     的渲染一致。Deployment 注解据此 sha256，密钥变更即触发滚动重启，
     避免签发方（user-service）与验签方（api-gateway）因 Pod 未重启而密钥错配。 */}}
{{- define "quenching.platformSecretMaterial" -}}
jwt-secret: {{ .Values.global.jwtSecret }}
db-dsn: {{ include "quenching.dbDsn" . }}
amqp-url: {{ include "quenching.amqpUrl" . }}
ws-internal-api-key: {{ .Values.observability.wsGatewayApiKey }}
{{- end -}}

{{/* 网关集群路由表：前缀 -> (Service, Port, 鉴权)。与 api-gateway 内置默认表对应，
     仅把 localhost:port 换成集群 Service DNS。useClusterRoutes=true 时渲染为 ROUTE_N。 */}}
{{- define "quenching.gatewayRoutes" -}}
{{- /* g7：/api/v1/events 与 /ws 已并入 api-gateway 进程，不再代理（此注释须在 toJson 输出之外，避免污染 JSON） */ -}}
{{- list
  (dict "prefix" "/api/v1/auth" "svc" "user-service" "port" 8085 "auth" false)
  (dict "prefix" "/api/v1/users" "svc" "user-service" "port" 8085 "auth" true)
  (dict "prefix" "/api/v1/admin" "svc" "user-service" "port" 8085 "auth" true)
  (dict "prefix" "/api/v1/projects" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/project-services" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/templates" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/template-media" "svc" "content-service" "port" 8082 "auth" false)
  (dict "prefix" "/api/v1/admin/projects" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/admin/feedback" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/feedback" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/sandboxes" "svc" "sandbox-service" "port" 8089 "auth" true)
  (dict "prefix" "/api/admin/v1/resources" "svc" "sandbox-service" "port" 8089 "auth" true)
  (dict "prefix" "/api/admin/v1/sandboxes" "svc" "sandbox-service" "port" 8089 "auth" true)
  (dict "prefix" "/api/admin/v1/anomalies" "svc" "sandbox-service" "port" 8089 "auth" true)
  (dict "prefix" "/api/v1/ai" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/sessions" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/training" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/admin/pitfalls" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/scoring" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/difficulties" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/alerts" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/metrics" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/logs" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/logviewer" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/health" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/ab-stats" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/v1/recovery" "svc" "training-service" "port" 8086 "auth" true)
  (dict "prefix" "/api/admin/v1/defects" "svc" "content-service" "port" 8082 "auth" true)
  (dict "prefix" "/api/v1/reviews" "svc" "review-service" "port" 8087 "auth" true)
  (dict "prefix" "/api/v1/events" "svc" "review-service" "port" 8087 "auth" true)
  (dict "prefix" "/api/v1/funnels" "svc" "review-service" "port" 8087 "auth" true)
  (dict "prefix" "/api/v1/dashboard" "svc" "review-service" "port" 8087 "auth" true)
  (dict "prefix" "/api/v1/ai-behavior" "svc" "review-service" "port" 8087 "auth" true)
  (dict "prefix" "/api/v1/ai-board" "svc" "review-service" "port" 8087 "auth" true)
  | toJson -}}
{{- end -}}
