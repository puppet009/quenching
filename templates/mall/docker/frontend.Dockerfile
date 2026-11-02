FROM node:22-alpine3.21 AS build
WORKDIR /src
COPY upstream/mall-app-web/ ./
# Keep API calls relative to the storefront origin (including proxy prefixes).
ENV VITE_API_BASE_URL=api
ENV VITE_USE_ALIPAY=false
RUN npm ci --no-audit --no-fund && npm run build:h5

FROM build AS source-seed
RUN rm -rf /src/node_modules /src/dist

FROM nginx:1.27-alpine3.21
# Node and nginx use the same Alpine release; Node needs these shared libraries.
RUN apk add --no-cache libstdc++ libgcc
COPY --from=build /usr/local/bin/node /usr/local/bin/node
COPY --from=build /usr/local/lib/node_modules/ /usr/local/lib/node_modules/
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm && \
    ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
# Image seeds stay outside the class PVC; dependencies need not be copied to it.
COPY --from=source-seed /src/ /opt/training/frontend-source/
COPY adaptation/TRAINING.md /opt/training/frontend-source/TRAINING.md
COPY --from=build /src/node_modules/ /opt/training/frontend-node_modules/
COPY --from=build /src/dist/build/h5/ /opt/training/frontend-html/
COPY adaptation/nginx.conf /opt/training/nginx.conf
COPY adaptation/frontend-rebuild.sh /usr/local/bin/training-rebuild
COPY adaptation/frontend-entrypoint.sh /usr/local/bin/training-entrypoint
RUN chmod +x /usr/local/bin/training-rebuild /usr/local/bin/training-entrypoint && \
    ln -s training-rebuild /usr/local/bin/mall-rebuild && \
    rm -rf /usr/share/nginx/html && \
    rm /etc/nginx/conf.d/default.conf && \
    ln -s /workspace/runtime/current /usr/share/nginx/html && \
    ln -s /workspace/runtime/nginx.conf /etc/nginx/conf.d/default.conf && \
    node --version && npm --version
ENV NODE_OPTIONS="--max-old-space-size=640" \
    VITE_API_BASE_URL=api \
    VITE_USE_ALIPAY=false
WORKDIR /workspace
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/training-entrypoint"]
CMD ["nginx", "-g", "daemon off;"]
