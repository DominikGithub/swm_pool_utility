FROM node:24.22.3-alpine AS builder

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

COPY *.js index.html vite.config.js ./
COPY src ./src
COPY public ./public
RUN npm run build

FROM nginx:1.30.7-alpine
# Pull Alpine security fixes published after the base image was built.
RUN apk upgrade --no-cache
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
