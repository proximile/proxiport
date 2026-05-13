FROM alpine:latest as downloader

ARG proxiport_version=0.1.0
ARG NOVNC_VERSION=1.3.0

RUN apk add unzip

WORKDIR /app/

RUN wget -q https://github.com/proximile/proxiport/releases/download/v${proxiport_version}/proxiportd_${proxiport_version}_Linux_x86_64.tar.gz -O proxiportd.tar.gz \
     && tar xzf proxiportd.tar.gz proxiportd
# Frontend is built from source — see the `frontend-builder` stage below.
# (The upstream openrport project published a pre-built frontend zip at
# downloads.openrport.io; ProxiPort replaced that Vue/Nuxt SPA with the
# SvelteKit frontend in `frontend/` so we build it ourselves.)
RUN wget https://github.com/novnc/noVNC/archive/refs/tags/v${NOVNC_VERSION}.zip -O novnc.zip \
    && unzip novnc.zip && mv noVNC-${NOVNC_VERSION} ./novnc

FROM node:22-alpine as frontend-builder
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM alpine:latest

USER root

ARG TZ="UTC"
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && \
    echo $TZ > /etc/timezone

RUN apk add --no-cache gcompat wget supervisor guacamole-server shadow
    
RUN apk --purge del apk-tools && rm -rf /tmp/* /var/tmp/*


COPY --from=downloader /app/proxiportd /usr/local/bin/proxiportd
COPY --from=frontend-builder /src/build/ /var/www/html/
COPY --from=downloader /app/novnc/ /var/lib/proxiport-novnc
COPY supervisord.conf /etc/supervisord.conf

RUN useradd -d /var/lib/proxiport -m -U -r -s /bin/false proxiport

RUN touch /var/lib/proxiport/proxiport.log && chown proxiport /var/lib/proxiport/proxiport.log && touch /var/lib/proxiport/supervisord.log && chown proxiport /var/lib/proxiport/supervisord.log

USER proxiport

RUN chmod 755 -R /var/lib/proxiport/

EXPOSE 8080
EXPOSE 3000
EXPOSE 30000-35000
EXPOSE 4822

CMD ["/usr/bin/supervisord"]
