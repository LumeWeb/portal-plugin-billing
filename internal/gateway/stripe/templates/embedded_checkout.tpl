<div id="stripe-checkout-container">
	<div id="stripe-checkout"></div>
</div>
<script src="https://js.stripe.com/dahlia/stripe.js"></script>
<script>
(function() {
	const stripe = Stripe('{{.PublishableKey}}');

	async function initialize() {
		const checkout = await stripe.initEmbeddedCheckout({
			clientSecret: '{{.ClientSecret}}',
			appearance: { theme: '{{.Appearance}}' }
		});

		checkout.mount('#stripe-checkout');
	}

	initialize();
})();
</script>
