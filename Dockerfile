FROM busybox:musl AS dirs
RUN mkdir /cache && chown 65532:65532 /cache

FROM gcr.io/distroless/static-debian13:nonroot

ARG TARGETARCH
COPY --chmod=755 mnp-linux-${TARGETARCH} /usr/local/bin/mnp
COPY --from=dirs --chown=65532:65532 /cache /cache

ENV XDG_CACHE_HOME=/cache

EXPOSE 8080

ENTRYPOINT ["mnp"]
CMD ["serve"]
