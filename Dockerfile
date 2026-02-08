FROM gcr.io/distroless/static-debian13:nonroot

ARG TARGETARCH
COPY mnp-linux-${TARGETARCH} /usr/local/bin/mnp

ENV XDG_CACHE_HOME=/var/cache/mnp

VOLUME /var/cache/mnp

EXPOSE 8080

ENTRYPOINT ["mnp"]
CMD ["serve"]
