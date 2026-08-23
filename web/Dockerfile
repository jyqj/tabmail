FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
ARG INTERNAL_API_URL=http://tabmail:8080
ENV INTERNAL_API_URL=${INTERNAL_API_URL}
RUN npm run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
# `output: "standalone"` traces the server and only the node_modules it loads
# into .next/standalone, so the runtime image skips the full install. The static
# and public assets are not traced and have to be copied alongside it.
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public
EXPOSE 3000
CMD ["node", "server.js"]
