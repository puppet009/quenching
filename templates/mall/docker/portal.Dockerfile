FROM maven:3.9.9-eclipse-temurin-17 AS build
WORKDIR /src
COPY upstream/mall/ ./
COPY adaptation/LoadingPaymentPaths.java adaptation/LoadingStagePaymentFilter.java adaptation/SandboxPaymentController.java adaptation/SandboxPaymentSecurityConfig.java adaptation/SandboxPaymentSignature.java /src/mall-portal/src/main/java/com/macro/mall/portal/config/
# The upstream package phase also invokes a remote Docker plugin. Skip it.
RUN mvn -B -Dmaven.repo.local=/opt/training/m2 -pl mall-portal -am package -DskipTests -Ddocker.skip=true
# Verify the exact offline rebuild from clean source while image builds still
# have network access. Keep target output out of each student's workspace seed.
RUN mkdir -p /seed/mall && cp -a /src/. /seed/mall/ && \
    find /seed/mall -type d -name target -prune -exec rm -rf '{}' + && \
    cd /seed/mall && \
    mvn -o -B -Dmaven.repo.local=/opt/training/m2 -pl mall-portal -am package -DskipTests -Ddocker.skip=true && \
    find /seed/mall -type d -name target -prune -exec rm -rf '{}' +

FROM maven:3.9.9-eclipse-temurin-17
WORKDIR /app
COPY --from=build /seed/mall/ /opt/training/mall/
COPY adaptation/TRAINING.md /opt/training/mall/TRAINING.md
COPY --from=build /opt/training/m2/ /opt/training/m2/
COPY --from=build /src/mall-portal/target/mall-portal-1.0-SNAPSHOT.jar /opt/training/runtime/mall-portal.jar
COPY adaptation/application-sandbox.yml /opt/training/runtime/config/application-sandbox.yml
COPY adaptation/logback-sandbox.xml /opt/training/runtime/config/logback-sandbox.xml
COPY adaptation/portal-entrypoint.sh /usr/local/bin/training-entrypoint
COPY adaptation/portal-rebuild.sh /usr/local/bin/training-rebuild
RUN chmod +x /usr/local/bin/training-entrypoint /usr/local/bin/training-rebuild && \
    ln -s training-rebuild /usr/local/bin/mall-rebuild && \
    ln -s /workspace/runtime/mall-portal.jar /app/mall-portal.jar && \
    ln -s /workspace/runtime/config /app/config && \
    mkdir -p /root/.m2 && \
    ln -s /opt/training/m2 /root/.m2/repository && \
    java -version && mvn --version
EXPOSE 8085
ENV SPRING_PROFILES_ACTIVE=sandbox
# /workspace is mounted as an empty PVC before the first entrypoint runs.
# Stay outside it until training-entrypoint has created the source directory.
ENTRYPOINT ["/usr/local/bin/training-entrypoint"]
