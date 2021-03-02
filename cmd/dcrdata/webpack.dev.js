const { merge } = require('webpack-merge')
const common = require('./webpack.common.js')
const path = require('path')
const ESLintPlugin = require('eslint-webpack-plugin')

module.exports = merge(common, {
  mode: 'development',
  plugins: [new ESLintPlugin({
    formatter: 'stylish'
  })],
  devtool: 'inline-source-map',
  devServer: {
    static: path.resolve(__dirname, 'public/index.js'), // './public/index.js',
    port: 7777,
    watch: true
  }
})
