# M3U8 Proxy

An m3u8 proxy server for HLS live streams that can "freeze" a stream in place, creating a VOD out of a live stream.

## Command line parameters

- `-listen`: The interface to listen on, default :8080
- `-upstream`: The upstream server to proxy requests to, where the actual HLS segments will be stored. Required.

## HTTP interface

All requests will be proxied to the backend server except for playlist requests with a `start` or `end` parameter. If a proxy request to a playlist has a start or an end parameter, the m3u8 will be modified to only include segments that fall between start and end, inclusive. For best results, configure your HLS generator to either have a start time parameter, or write your segments with the Unix timestamp as their segment number.