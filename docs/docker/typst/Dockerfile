ARG TYPST_VERSION=latest
FROM ghcr.io/typst/typst:${TYPST_VERSION}

LABEL authors="jeremie"

RUN apk add --no-cache \
        font-liberation \
        font-roboto \
        font-linux-libertine

ENTRYPOINT ["typst"]
CMD ["--help"]