ARG TARGETARCH

FROM scratch
ARG TARGETARCH
ARG VERSION=unknown
ARG REVISION=unknown
LABEL org.opencontainers.image.title="Worklease" \
      org.opencontainers.image.description="Worklease remote claim authority" \
      org.opencontainers.image.source="https://github.com/brettinternet/worklease" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --chown=65532:65532 bin/worklease-${TARGETARCH} /worklease
USER 65532:65532
EXPOSE 7443
ENTRYPOINT ["/worklease"]
CMD ["serve", "--server-config", "/run/worklease/server.yaml"]
